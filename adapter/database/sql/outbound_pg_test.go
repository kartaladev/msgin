package sql_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	msgin "github.com/kartaladev/msgin"
	msginsql "github.com/kartaladev/msgin/adapter/database/sql"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

// OutboundSuite provisions one container per engine for the whole suite and
// exercises the Outbound INSERT adapter end-to-end — as a direct
// msgin.NewProducer target, paired with a Source for the produce/consume
// round-trip, and as a msgin.RetryPolicy.DeadLetter / msgin.WithInvalidMessageSink
// target. TestOutboundSuite runs it once per engine (postgres, mysql) so the SAME
// assertions prove the Outbound works over both dialects. Each test uses a
// freshly-named, freshly-provisioned table so cases stay isolated. TestMain
// (goroutine-leak check) is declared once for the whole package in
// source_pg_test.go.
type OutboundSuite struct {
	suite.Suite
	engine  engine
	db      *sql.DB
	dialect msginsql.Dialect
	counter atomic.Int64
}

func TestOutboundSuite(t *testing.T) {
	for _, e := range engines {
		t.Run(e.name, func(t *testing.T) {
			suite.Run(t, &OutboundSuite{engine: e})
		})
	}
}

func (s *OutboundSuite) SetupSuite() {
	s.db = s.engine.openDB(s.T())
	s.dialect = s.engine.dialect
}

// freshTable returns a unique, schema-applied table name for a single test.
func (s *OutboundSuite) freshTable(ctx context.Context) string {
	name := fmt.Sprintf("msgin_out_%d", s.counter.Add(1))
	require.NoError(s.T(), s.dialect.EnsureSchema(ctx, s.db, name))
	return name
}

// insertJSON frames headers carrying id and inserts one immediately-visible
// message whose payload is the JSON encoding of v (matching the consumer's
// default JSON codec) — used to seed a source table directly, bypassing the
// Outbound under test.
func (s *OutboundSuite) insertJSON(ctx context.Context, table, id string, v any) {
	t := s.T()
	headers, err := msginsql.EncodeHeaders(msgin.NewHeaders(map[string]any{msgin.HeaderID: id}))
	require.NoError(t, err)
	payload, err := json.Marshal(v)
	require.NoError(t, err)
	require.NoError(t, s.dialect.Insert(ctx, s.db, table, id, headers, payload, 0))
}

// rowByMsgID returns the framed headers and payload for the row identified by
// msgID, and whether it was found.
func (s *OutboundSuite) rowByMsgID(ctx context.Context, table, msgID string) (headers, payload []byte, ok bool) {
	t := s.T()
	var h string
	var p []byte
	err := s.db.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT %s, payload FROM %s WHERE msg_id = %s`,
			s.engine.headersTextExpr("headers"), s.engine.quote(table), s.engine.ph(1)), msgID).Scan(&h, &p)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, false
	}
	require.NoError(t, err)
	return []byte(h), p, true
}

// rowCount returns the number of rows in table.
func (s *OutboundSuite) rowCount(ctx context.Context, table string) int {
	var n int
	require.NoError(s.T(), s.db.QueryRowContext(ctx, fmt.Sprintf(`SELECT count(*) FROM %s`, s.engine.quote(table))).Scan(&n))
	return n
}

// ---- tests --------------------------------------------------------------

// TestProducerSendInsertsRowWithHeadersAndPayload proves the Outbound's
// primary hot path: msgin.NewProducer.Send frames the message through
// Outbound.Send into a row whose msg_id, decoded headers, and JSON-encoded
// payload all match what was produced.
func (s *OutboundSuite) TestProducerSendInsertsRowWithHeadersAndPayload() {
	ctx := s.T().Context()
	t := s.T()
	table := s.freshTable(ctx)

	out, err := msginsql.NewOutboundAdapter(s.db, table)
	require.NoError(t, err)

	producer, err := msgin.NewProducer[string](out)
	require.NoError(t, err)

	msg := msgin.New("hello-world", msgin.WithID("prod-1"), msgin.WithHeaders(map[string]any{"custom": "value"}))
	require.NoError(t, producer.Send(ctx, msg))

	headers, payload, ok := s.rowByMsgID(ctx, table, "prod-1")
	require.True(t, ok, "the produced row must be present")

	decoded, err := msginsql.DecodeHeaders(headers)
	require.NoError(t, err)
	gotID, hasID := decoded.String(msgin.HeaderID)
	require.True(t, hasID)
	require.Equal(t, "prod-1", gotID)
	custom, hasCustom := decoded.String("custom")
	require.True(t, hasCustom)
	require.Equal(t, "value", custom)

	var gotPayload string
	require.NoError(t, json.Unmarshal(payload, &gotPayload))
	require.Equal(t, "hello-world", gotPayload)
}

// TestRoundTripProduceThenConsume is the key test proving the Outbound and
// Task-5 Source form a working produce/consume pair on the same table: a
// message produced via Outbound.Send (through msgin.NewProducer) is polled,
// decoded back to T, handled, Acked, and its row is deleted.
func (s *OutboundSuite) TestRoundTripProduceThenConsume() {
	ctx := s.T().Context()
	t := s.T()
	table := s.freshTable(ctx)

	out, err := msginsql.NewOutboundAdapter(s.db, table)
	require.NoError(t, err)
	producer, err := msgin.NewProducer[string](out)
	require.NoError(t, err)

	require.NoError(t, producer.Send(ctx, msgin.New("round-trip-payload", msgin.WithID("rt-1"))))
	require.Equal(t, 1, s.rowCount(ctx, table), "the produced row is visible before consuming")

	src, err := msginsql.NewPollingSource(s.db, table)
	require.NoError(t, err)

	var mu sync.Mutex
	var got string
	done := make(chan struct{})
	var once sync.Once
	handler := func(_ context.Context, msg msgin.Message[string]) error {
		mu.Lock()
		got = msg.Payload()
		mu.Unlock()
		once.Do(func() { close(done) })
		return nil
	}
	consumer, err := msgin.NewConsumer[string](src, handler,
		msgin.WithPollInterval[string](200*time.Millisecond),
		msgin.WithShutdownTimeout[string](10*time.Second),
	)
	require.NoError(t, err)

	runErr := runConsumerUntil(t, ctx, consumer, done, 20*time.Second)
	require.ErrorIs(t, runErr, context.Canceled)

	mu.Lock()
	require.Equal(t, "round-trip-payload", got, "the round-tripped payload decodes back to T")
	mu.Unlock()
	require.Equal(t, 0, s.rowCount(ctx, table), "the row is Acked (DELETEd) after handling")
}

// TestPoisonMessageLandsInSinkTable proves the Outbound is usable directly as
// a msgin.RetryPolicy.DeadLetter sink AND as a msgin.WithInvalidMessageSink
// target: in both cases the failing message's row lands in the DLQ table and
// the source row is settled away (Acked via the divert).
func (s *OutboundSuite) TestPoisonMessageLandsInSinkTable() {
	ctx := s.T().Context()

	type testCase struct {
		name        string
		handlerErr  error
		buildOpts   func(dlq *msginsql.Outbound) []msgin.ConsumerOption[string]
		description string
	}

	cases := []testCase{
		{
			name:       "MaxAttempts exhaustion diverts to RetryPolicy.DeadLetter",
			handlerErr: fmt.Errorf("transient boom"),
			buildOpts: func(dlq *msginsql.Outbound) []msgin.ConsumerOption[string] {
				return []msgin.ConsumerOption[string]{
					msgin.WithRetryPolicy[string](msgin.RetryPolicy{MaxAttempts: 1, DeadLetter: dlq}),
					msgin.WithPollInterval[string](200 * time.Millisecond),
					msgin.WithShutdownTimeout[string](10 * time.Second),
				}
			},
		},
		{
			name:       "Permanent handler error diverts to WithInvalidMessageSink",
			handlerErr: msgin.Permanent(errors.New("poison")),
			buildOpts: func(dlq *msginsql.Outbound) []msgin.ConsumerOption[string] {
				return []msgin.ConsumerOption[string]{
					msgin.WithInvalidMessageSink[string](dlq),
					msgin.WithPollInterval[string](200 * time.Millisecond),
					msgin.WithShutdownTimeout[string](10 * time.Second),
				}
			},
		},
	}

	for _, tc := range cases {
		s.Run(tc.name, func() {
			t := s.T()
			table := s.freshTable(ctx)
			dlqTable := s.freshTable(ctx)

			s.insertJSON(ctx, table, "poison-1", "bad-payload")

			src, err := msginsql.NewPollingSource(s.db, table)
			require.NoError(t, err)

			dlq, err := msginsql.NewOutboundAdapter(s.db, dlqTable)
			require.NoError(t, err)

			handler := func(_ context.Context, _ msgin.Message[string]) error {
				return tc.handlerErr
			}

			consumer, err := msgin.NewConsumer[string](src, handler, tc.buildOpts(dlq)...)
			require.NoError(t, err)

			runCtx, cancel := context.WithCancel(ctx)
			errCh := make(chan error, 1)
			go func() { errCh <- consumer.Run(runCtx) }()

			require.Eventually(t, func() bool {
				return s.rowCount(ctx, dlqTable) == 1
			}, 15*time.Second, 100*time.Millisecond, "expected the poison message to land in the DLQ table")

			cancel()
			require.ErrorIs(t, <-errCh, context.Canceled)

			require.Equal(t, 0, s.rowCount(ctx, table), "the source row is settled away (Acked) after the divert")

			headers, payload, ok := s.rowByMsgID(ctx, dlqTable, "poison-1")
			require.True(t, ok, "the poison message's row must be present in the DLQ table")

			decoded, err := msginsql.DecodeHeaders(headers)
			require.NoError(t, err)
			gotID, _ := decoded.String(msgin.HeaderID)
			require.Equal(t, "poison-1", gotID)

			var gotPayload string
			require.NoError(t, json.Unmarshal(payload, &gotPayload))
			require.Equal(t, "bad-payload", gotPayload)
		})
	}
}

// TestReadyAndSendSurfaceSchemaNotReady: an un-provisioned table fails fast at
// Ready and Send wraps the INSERT failure as the same portable
// ErrSchemaNotReady, naming the table.
func (s *OutboundSuite) TestReadyAndSendSurfaceSchemaNotReady() {
	ctx := s.T().Context()
	t := s.T()

	// A valid identifier that was never created.
	table := fmt.Sprintf("msgin_out_missing_%d", s.counter.Add(1))
	out, err := msginsql.NewOutboundAdapter(s.db, table)
	require.NoError(t, err)

	require.ErrorIs(t, out.Ready(ctx), msginsql.ErrSchemaNotReady)

	msg := msgin.NewMessage[any](any([]byte(`"payload"`)),
		msgin.NewHeaders(map[string]any{msgin.HeaderID: "missing-1"}))

	sendErr := out.Send(ctx, msg)
	require.ErrorIs(t, sendErr, msginsql.ErrSchemaNotReady)
	require.Contains(t, sendErr.Error(), table, "error names the offending table")
}

// TestSendFramingAndPayloadErrors covers Send's defensive/error branches: a
// non-[]byte payload (ErrInvalidPayload — the producer always guarantees
// []byte for a wire adapter, so this is a direct-call defensive case) and an
// unencodable header value (a framing failure from EncodeHeaders), both
// surfaced BEFORE any INSERT is attempted.
func (s *OutboundSuite) TestSendFramingAndPayloadErrors() {
	ctx := s.T().Context()

	type testCase struct {
		name   string
		msg    msgin.Message[any]
		assert func(t *testing.T, err error)
	}

	cases := []testCase{
		{
			name: "non-[]byte payload returns ErrInvalidPayload",
			msg: msgin.NewMessage[any](any("not-bytes"),
				msgin.NewHeaders(map[string]any{msgin.HeaderID: "bad-payload-1"})),
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msginsql.ErrInvalidPayload)
			},
		},
		{
			name: "unencodable header value fails framing",
			msg: msgin.NewMessage[any](any([]byte("ok")),
				msgin.NewHeaders(map[string]any{msgin.HeaderID: "bad-header-1", "custom": make(chan int)})),
			assert: func(t *testing.T, err error) {
				require.Error(t, err)
				require.NotErrorIs(t, err, msginsql.ErrInvalidPayload)
				require.NotErrorIs(t, err, msginsql.ErrSchemaNotReady)
			},
		},
	}

	for _, tc := range cases {
		s.Run(tc.name, func() {
			t := s.T()
			table := s.freshTable(ctx)
			out, err := msginsql.NewOutboundAdapter(s.db, table)
			require.NoError(t, err)

			sendErr := out.Send(ctx, tc.msg)
			tc.assert(t, sendErr)
			require.Equal(t, 0, s.rowCount(ctx, table), "a rejected Send must not insert a row")
		})
	}
}
