package sql_test

// outbound_unit_test.go drives Outbound against the in-memory fakeDialect
// (fakedialect_test.go) — no real database (Plan 006 Task 2). It covers
// Send's payload/framing branch and every resolveQuerier branch (ADR 0010
// D8): no-resolver, resolver-err, shared-tx, strict-no-tx, and
// opportunistic-no-tx, plus the classifyQueryErr wrap-vs-passthrough branch.

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	msgin "github.com/kartaladev/msgin"
	msginsql "github.com/kartaladev/msgin/adapter/database/sql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestOutbound_WithFakeDialect proves the fake satisfies LeaseDialect at
// Outbound construction (the exhaustive nil/invalid-table matrix stays
// covered against the built-ins in outbound_test.go, Task 1).
func TestOutbound_WithFakeDialect(t *testing.T) {
	t.Parallel()

	out, err := msginsql.NewOutboundAdapter(openDB(t, fakeDriverName), "msgs", newFakeDialect())
	require.NoError(t, err)
	assert.NotNil(t, out)
}

// TestOutbound_SendFramesPayload covers Send's payload/framing branch: a
// valid []byte payload is inserted verbatim with its headers round-trippable
// via DecodeHeaders, and a non-[]byte payload is rejected with
// ErrInvalidPayload before any dialect call.
func TestOutbound_SendFramesPayload(t *testing.T) {
	t.Parallel()

	t.Run("a valid []byte payload is framed and inserted", func(t *testing.T) {
		t.Parallel()
		fd := newFakeDialect()
		out, err := msginsql.NewOutboundAdapter(openDB(t, fakeDriverName), "msgs", fd)
		require.NoError(t, err)

		msg := msgin.New[any]([]byte(`{"k":"v"}`), msgin.WithID("m-42"))
		require.NoError(t, out.Send(t.Context(), msg))

		row := fd.onlyRow(t)
		assert.Equal(t, "m-42", row.msgID)
		assert.Equal(t, []byte(`{"k":"v"}`), row.payload)

		decoded, err := msginsql.DecodeHeaders(row.headers)
		require.NoError(t, err)
		id, ok := decoded.String(msgin.HeaderMessageID)
		require.True(t, ok)
		assert.Equal(t, "m-42", id)
	})

	t.Run("a non-[]byte payload is ErrInvalidPayload; no insert attempted", func(t *testing.T) {
		t.Parallel()
		fd := newFakeDialect()
		out, err := msginsql.NewOutboundAdapter(openDB(t, fakeDriverName), "msgs", fd)
		require.NoError(t, err)

		msg := msgin.New[any](12345, msgin.WithID("m-bad"))
		err = out.Send(t.Context(), msg)
		require.ErrorIs(t, err, msginsql.ErrInvalidPayload)
		assert.Zero(t, fd.rowCount())
	})
}

// TestOutbound_ResolveQuerier covers every resolveQuerier branch (ADR 0010
// D8): no shared-tx option configured, the resolver itself erroring, a
// resolved shared transaction present, strict mode with none present
// (ErrNoSharedTransaction), and opportunistic mode with none present
// (falls back to the pool db).
func TestOutbound_ResolveQuerier(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name   string
		opts   func(db *sql.DB) []msginsql.Option
		assert func(t *testing.T, fd *fakeDialect, db *sql.DB, err error)
	}

	cases := []testCase{
		{
			name: "no resolver configured: inserts on the pool db",
			opts: func(db *sql.DB) []msginsql.Option { return nil },
			assert: func(t *testing.T, fd *fakeDialect, db *sql.DB, err error) {
				require.NoError(t, err)
				assert.Same(t, db, fd.lastInsertQuerier)
			},
		},
		{
			name: "resolver error: wrapped and returned, no insert attempted",
			opts: func(db *sql.DB) []msginsql.Option {
				return []msginsql.Option{msginsql.WithSharedTransaction(func(context.Context) (msginsql.Querier, error) {
					return nil, errors.New("resolver boom")
				})}
			},
			assert: func(t *testing.T, fd *fakeDialect, db *sql.DB, err error) {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "resolver boom")
				assert.Nil(t, fd.lastInsertQuerier, "Send must not insert when the resolver errors")
			},
		},
		{
			name: "shared tx present: inserts on the resolved Querier",
			opts: func(db *sql.DB) []msginsql.Option {
				return []msginsql.Option{msginsql.WithSharedTransaction(func(context.Context) (msginsql.Querier, error) {
					return fakeQuerier{}, nil
				})}
			},
			assert: func(t *testing.T, fd *fakeDialect, db *sql.DB, err error) {
				require.NoError(t, err)
				assert.Equal(t, fakeQuerier{}, fd.lastInsertQuerier)
			},
		},
		{
			name: "strict mode, no tx present: ErrNoSharedTransaction, no insert attempted",
			opts: func(db *sql.DB) []msginsql.Option {
				return []msginsql.Option{msginsql.WithSharedTransaction(func(context.Context) (msginsql.Querier, error) {
					return nil, nil
				})}
			},
			assert: func(t *testing.T, fd *fakeDialect, db *sql.DB, err error) {
				require.ErrorIs(t, err, msginsql.ErrNoSharedTransaction)
				assert.Nil(t, fd.lastInsertQuerier)
			},
		},
		{
			name: "opportunistic mode, no tx present: falls back to the pool db",
			opts: func(db *sql.DB) []msginsql.Option {
				return []msginsql.Option{msginsql.WithOpportunisticSharedTransaction(func(context.Context) (msginsql.Querier, error) {
					return nil, nil
				})}
			},
			assert: func(t *testing.T, fd *fakeDialect, db *sql.DB, err error) {
				require.NoError(t, err)
				assert.Same(t, db, fd.lastInsertQuerier)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fd := newFakeDialect()
			db := openDB(t, fakeDriverName)
			out, err := msginsql.NewOutboundAdapter(db, "msgs", fd, tc.opts(db)...)
			require.NoError(t, err)

			msg := msgin.New[any]([]byte("payload"), msgin.WithID("m-1"))
			sendErr := out.Send(t.Context(), msg)
			tc.assert(t, fd, db, sendErr)
		})
	}
}

// TestOutbound_SendAfter_ThreadsDelay covers the ScheduledSender capability:
// SendAfter sets visible_after = db-now + delay (recorded by fakeDialect), and
// Send is exactly SendAfter with delay 0. now() is fixed for a deterministic
// visible_after assertion.
func TestOutbound_SendAfter_ThreadsDelay(t *testing.T) {
	t.Parallel()
	epoch := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	type testCase struct {
		name   string
		send   func(ctx context.Context, out *msginsql.Outbound, msg msgin.Message[any]) error
		assert func(t *testing.T, visibleAfter time.Time)
	}
	cases := []testCase{
		{
			name: "Send inserts an immediately visible row (delay 0)",
			send: func(ctx context.Context, out *msginsql.Outbound, msg msgin.Message[any]) error {
				return out.Send(ctx, msg)
			},
			assert: func(t *testing.T, va time.Time) { assert.Equal(t, epoch, va) },
		},
		{
			name: "SendAfter sets visible_after = now + delay",
			send: func(ctx context.Context, out *msginsql.Outbound, msg msgin.Message[any]) error {
				return out.SendAfter(ctx, msg, 30*time.Minute)
			},
			assert: func(t *testing.T, va time.Time) { assert.Equal(t, epoch.Add(30*time.Minute), va) },
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fd := newFakeDialect()
			fd.now = func() time.Time { return epoch }
			out, err := msginsql.NewOutboundAdapter(openDB(t, fakeDriverName), "msgs", fd)
			require.NoError(t, err)

			msg := msgin.New[any]([]byte(`{"k":"v"}`), msgin.WithID("s-1"))
			require.NoError(t, tc.send(t.Context(), out, msg))
			tc.assert(t, fd.onlyRow(t).visibleAfter)
		})
	}
}

// TestOutbound_IsScheduledSender documents the capability at the type level.
func TestOutbound_IsScheduledSender(t *testing.T) {
	t.Parallel()
	var _ msgin.ScheduledSender = (*msginsql.Outbound)(nil)
}

// TestOutbound_SendClassifiesInsertError covers Outbound.Send's
// classifyQueryErr wrap-vs-passthrough branch: an Insert failure is wrapped
// ErrSchemaNotReady iff the table is missing, otherwise it propagates raw.
func TestOutbound_SendClassifiesInsertError(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name        string
		schemaReady bool
		assert      func(t *testing.T, err error)
	}

	cases := []testCase{
		{
			name:        "schema missing: wrapped ErrSchemaNotReady",
			schemaReady: false,
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msginsql.ErrSchemaNotReady)
			},
		},
		{
			name:        "schema present: raw error propagates",
			schemaReady: true,
			assert: func(t *testing.T, err error) {
				require.Error(t, err)
				assert.NotErrorIs(t, err, msginsql.ErrSchemaNotReady)
				assert.Contains(t, err.Error(), "insert boom")
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fd := newFakeDialect()
			if tc.schemaReady {
				fd.markReady("msgs")
			}
			fd.insertErr = errors.New("insert boom")
			out, err := msginsql.NewOutboundAdapter(openDB(t, fakeDriverName), "msgs", fd)
			require.NoError(t, err)

			msg := msgin.New[any]([]byte("p"), msgin.WithID("m-1"))
			sendErr := out.Send(t.Context(), msg)
			tc.assert(t, sendErr)
		})
	}
}
