package harness

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	msgin "github.com/kartaladev/msgin"
	msginsql "github.com/kartaladev/msgin/adapter/database/sql"
	"github.com/stretchr/testify/require"
)

// ---- shared table/row helpers -------------------------------------------

// freshTable returns a unique, schema-applied Source/Outbound/Outbox table
// name for a single (sub)test, mirroring the pre-split suites' freshTable
// helper. prefix distinguishes the caller (e.g. "msgin_src", "msgin_out") so
// table names stay descriptive across a run.
func freshTable(t *testing.T, ctx context.Context, kit TestKit, db *sql.DB, counter *atomic.Int64, prefix string) string {
	t.Helper()
	name := fmt.Sprintf("%s_%d", prefix, counter.Add(1))
	require.NoError(t, kit.Lease.EnsureSchema(ctx, db, name))
	return name
}

// insertJSON frames headers carrying id and inserts one immediately-visible
// message whose payload is the JSON encoding of v (matching the runtime's
// default JSON codec) directly through the dialect under test — used to seed
// a table without going through the adapter under test.
func insertJSON(t *testing.T, ctx context.Context, kit TestKit, db *sql.DB, table, id string, v any) {
	t.Helper()
	headers, err := msginsql.EncodeHeaders(msgin.NewHeaders(map[string]any{msgin.HeaderID: id}))
	require.NoError(t, err)
	payload, err := json.Marshal(v)
	require.NoError(t, err)
	require.NoError(t, kit.Lease.Insert(ctx, db, table, id, headers, payload, 0))
}

// rowCount returns the number of rows in table on db.
func rowCount(t *testing.T, ctx context.Context, kit TestKit, db *sql.DB, table string) int {
	t.Helper()
	var n int
	require.NoError(t, db.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT count(*) FROM %s`, kit.Quote(table))).Scan(&n))
	return n
}

// requireNoConnLeak asserts the pool has no in-use connections at rest —
// every lock-strategy tx (commit or rollback) must return its pinned
// connection (ADR 0010 D5).
func requireNoConnLeak(t *testing.T, db *sql.DB) {
	t.Helper()
	require.Eventually(t, func() bool {
		return db.Stats().InUse == 0
	}, 5*time.Second, 20*time.Millisecond, "a lock tx leaked a pooled connection (InUse never returned to 0)")
}

// splitStatements splits a reference-DDL string into individual statements on
// the ";" boundary, dropping empty pieces — what a migration tool does when
// it runs each statement on its own (pgx's extended protocol rejects
// multi-statement Exec).
func splitStatements(ddl string) []string {
	var out []string
	for _, part := range strings.Split(ddl, ";") {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// ---- recording logger (concurrency-safe) --------------------------------

// syncBuffer is a mutex-guarded io.Writer so a slog.TextHandler written from
// multiple worker goroutines under -race stays safe, and the accumulated text
// can be inspected mid-test.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func newRecorder() (*slog.Logger, *syncBuffer) {
	sb := &syncBuffer{}
	return slog.New(slog.NewTextHandler(sb, &slog.HandlerOptions{Level: slog.LevelWarn})), sb
}

// ---- recording DLQ sink ---------------------------------------------------

// recordingSink is a concurrency-safe in-memory msgin.OutboundAdapter, used as
// a dead-letter sink in assertions where the sink itself must not add a pool
// dependency (e.g. the lock-strategy always-commit-Nack regression).
type recordingSink struct {
	mu   sync.Mutex
	msgs []msgin.Message[any]
}

func (r *recordingSink) Send(_ context.Context, msg msgin.Message[any]) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.msgs = append(r.msgs, msg)
	return nil
}

func (r *recordingSink) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.msgs)
}

// ---- consumer run helper ---------------------------------------------------

// runConsumerUntil runs consumer until done is signalled (or the deadline),
// then cancels and returns Run's error. It joins the Run goroutine so no
// goroutine outlives the test.
func runConsumerUntil(t *testing.T, ctx context.Context, consumer msgin.Consumer[string], done <-chan struct{}, deadline time.Duration) error {
	t.Helper()
	runCtx, cancel := context.WithCancel(ctx)
	errCh := make(chan error, 1)
	go func() { errCh <- consumer.Run(runCtx) }()

	select {
	case <-done:
	case <-time.After(deadline):
		cancel()
		<-errCh
		t.Fatal("timed out waiting for the consumer to process the expected messages")
		return nil
	}
	cancel()
	return <-errCh
}
