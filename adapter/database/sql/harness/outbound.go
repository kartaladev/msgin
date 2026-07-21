package harness

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
)

// RunOutbound exercises the INSERT msginsql.Outbound channel adapter against
// kit.Lease on the already-open db, reproducing every assertion the pre-split
// OutboundSuite made (Plan 006 Task 3): the producer hot path (frames headers
// + JSON payload into a row), the produce/consume round-trip with a paired
// Source on the same table, use as a msgin.RetryPolicy.DeadLetter AND a
// msgin.WithInvalidMessageSink target, Ready/Send surfacing
// ErrSchemaNotReady, and Send's defensive framing/payload error branches.
func RunOutbound(t *testing.T, kit TestKit, db *sql.DB) {
	t.Helper()
	var counter atomic.Int64
	fresh := func(ctx context.Context) string { return freshTable(t, ctx, kit, db, &counter, "msgin_out") }

	rowByMsgID := func(t *testing.T, ctx context.Context, table, msgID string) (headers, payload []byte, ok bool) {
		t.Helper()
		var h string
		var p []byte
		err := db.QueryRowContext(ctx,
			fmt.Sprintf(`SELECT %s, payload FROM %s WHERE msg_id = %s`,
				kit.HeadersTextExpr("headers"), kit.Quote(table), kit.Placeholder(1)), msgID).Scan(&h, &p)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil, false
		}
		require.NoError(t, err)
		return []byte(h), p, true
	}

	t.Run("ProducerSendInsertsRowWithHeadersAndPayload", func(t *testing.T) {
		ctx := t.Context()
		table := fresh(ctx)

		out, err := msginsql.NewOutboundAdapter(db, table, kit.Lease)
		require.NoError(t, err)

		producer, err := msgin.NewProducer[string](out)
		require.NoError(t, err)

		msg := msgin.New("hello-world", msgin.WithID("prod-1"), msgin.WithHeaders(map[string]any{"custom": "value"}))
		require.NoError(t, producer.Send(ctx, msg))

		headers, payload, ok := rowByMsgID(t, ctx, table, "prod-1")
		require.True(t, ok, "the produced row must be present")

		decoded, err := msginsql.DecodeHeaders(headers)
		require.NoError(t, err)
		gotID, hasID := decoded.String(msgin.HeaderMessageID)
		require.True(t, hasID)
		require.Equal(t, "prod-1", gotID)
		custom, hasCustom := decoded.String("custom")
		require.True(t, hasCustom)
		require.Equal(t, "value", custom)

		var gotPayload string
		require.NoError(t, json.Unmarshal(payload, &gotPayload))
		require.Equal(t, "hello-world", gotPayload)
	})

	t.Run("RoundTripProduceThenConsume", func(t *testing.T) {
		ctx := t.Context()
		table := fresh(ctx)

		out, err := msginsql.NewOutboundAdapter(db, table, kit.Lease)
		require.NoError(t, err)
		producer, err := msgin.NewProducer[string](out)
		require.NoError(t, err)

		require.NoError(t, producer.Send(ctx, msgin.New("round-trip-payload", msgin.WithID("rt-1"))))
		require.Equal(t, 1, rowCount(t, ctx, kit, db, table), "the produced row is visible before consuming")

		src, err := msginsql.NewPollingSource(db, table, kit.Lease)
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
		require.Equal(t, 0, rowCount(t, ctx, kit, db, table), "the row is Acked (DELETEd) after handling")
	})

	t.Run("ScheduledSendDelaysVisibility", func(t *testing.T) {
		ctx := t.Context()
		table := fresh(ctx)

		out, err := msginsql.NewOutboundAdapter(db, table, kit.Lease)
		require.NoError(t, err)
		p, err := msgin.NewProducer[string](out)
		require.NoError(t, err)
		src, err := msginsql.NewPollingSource(db, table, kit.Lease)
		require.NoError(t, err)

		// (a) A far-future delayed row is persisted but NOT claimable. Using a 1h
		// delay makes the invisibility assertion race-free (no plausible test/CI
		// stall approaches 1h between INSERT and the first Claim — F1), unlike a
		// short delay whose window a slow container could exceed.
		require.NoError(t, p.SendAfter(ctx, msgin.New("far"), 1*time.Hour))
		require.Equal(t, 1, rowCount(t, ctx, kit, db, table), "the row is persisted immediately")
		got, err := src.Poll(ctx, 10)
		require.NoError(t, err)
		require.Empty(t, got, "a far-future row must not be claimable before its visible_after")

		// (b) A short-delay row becomes claimable after its delay elapses; the
		// far-future row stays invisible. Eventually waits real time for (b) only.
		require.NoError(t, p.SendAfter(ctx, msgin.New("soon", msgin.WithID("soon")), 500*time.Millisecond))
		require.Eventually(t, func() bool {
			// No require.* here: testify runs this in a spawned goroutine, where a
			// FailNow/Goexit is unsupported (F3). Return false on any mismatch.
			d, err := src.Poll(ctx, 10)
			if err != nil || len(d) != 1 || d[0].Msg.ID() != "soon" {
				return false
			}
			return d[0].Ack(ctx) == nil
		}, 5*time.Second, 50*time.Millisecond, "the short-delay row becomes claimable after its delay")

		// The far-future row is still present and unclaimed (soon was Acked/deleted).
		require.Equal(t, 1, rowCount(t, ctx, kit, db, table), "the far-future row remains invisible")
	})

	t.Run("PoisonMessageLandsInSinkTable", func(t *testing.T) {
		ctx := t.Context()

		cases := []struct {
			name       string
			handlerErr error
			buildOpts  func(dlq *msginsql.Outbound) []msgin.ConsumerOption[string]
		}{
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
			t.Run(tc.name, func(t *testing.T) {
				table := fresh(ctx)
				dlqTable := fresh(ctx)

				insertJSON(t, ctx, kit, db, table, "poison-1", "bad-payload")

				src, err := msginsql.NewPollingSource(db, table, kit.Lease)
				require.NoError(t, err)

				dlq, err := msginsql.NewOutboundAdapter(db, dlqTable, kit.Lease)
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
					return rowCount(t, ctx, kit, db, dlqTable) == 1
				}, 15*time.Second, 100*time.Millisecond, "expected the poison message to land in the DLQ table")

				cancel()
				require.ErrorIs(t, <-errCh, context.Canceled)

				require.Equal(t, 0, rowCount(t, ctx, kit, db, table), "the source row is settled away (Acked) after the divert")

				headers, payload, ok := rowByMsgID(t, ctx, dlqTable, "poison-1")
				require.True(t, ok, "the poison message's row must be present in the DLQ table")

				decoded, err := msginsql.DecodeHeaders(headers)
				require.NoError(t, err)
				gotID, _ := decoded.String(msgin.HeaderMessageID)
				require.Equal(t, "poison-1", gotID)

				var gotPayload string
				require.NoError(t, json.Unmarshal(payload, &gotPayload))
				require.Equal(t, "bad-payload", gotPayload)
			})
		}
	})

	t.Run("ReadyAndSendSurfaceSchemaNotReady", func(t *testing.T) {
		ctx := t.Context()
		table := fmt.Sprintf("msgin_out_missing_%d", counter.Add(1))
		out, err := msginsql.NewOutboundAdapter(db, table, kit.Lease)
		require.NoError(t, err)

		require.ErrorIs(t, out.Ready(ctx), msginsql.ErrSchemaNotReady)

		msg := msgin.NewMessage[any](any([]byte(`"payload"`)),
			msgin.NewHeaders(map[string]any{msgin.HeaderMessageID: "missing-1"}))

		sendErr := out.Send(ctx, msg)
		require.ErrorIs(t, sendErr, msginsql.ErrSchemaNotReady)
		require.Contains(t, sendErr.Error(), table, "error names the offending table")
	})

	t.Run("SendFramingAndPayloadErrors", func(t *testing.T) {
		ctx := t.Context()

		cases := []struct {
			name   string
			msg    msgin.Message[any]
			assert func(t *testing.T, err error)
		}{
			{
				name: "non-[]byte payload returns ErrInvalidPayload",
				msg: msgin.NewMessage[any](any("not-bytes"),
					msgin.NewHeaders(map[string]any{msgin.HeaderMessageID: "bad-payload-1"})),
				assert: func(t *testing.T, err error) {
					require.ErrorIs(t, err, msginsql.ErrInvalidPayload)
				},
			},
			{
				name: "unencodable header value fails framing",
				msg: msgin.NewMessage[any](any([]byte("ok")),
					msgin.NewHeaders(map[string]any{msgin.HeaderMessageID: "bad-header-1", "custom": make(chan int)})),
				assert: func(t *testing.T, err error) {
					require.Error(t, err)
					require.NotErrorIs(t, err, msginsql.ErrInvalidPayload)
					require.NotErrorIs(t, err, msginsql.ErrSchemaNotReady)
				},
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				table := fresh(ctx)
				out, err := msginsql.NewOutboundAdapter(db, table, kit.Lease)
				require.NoError(t, err)

				sendErr := out.Send(ctx, tc.msg)
				tc.assert(t, sendErr)
				require.Equal(t, 0, rowCount(t, ctx, kit, db, table), "a rejected Send must not insert a row")
			})
		}
	})
}
