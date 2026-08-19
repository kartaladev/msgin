package sql_test

import (
	"database/sql"
	"testing"

	msgin "github.com/kartaladev/msgin"
	msginsql "github.com/kartaladev/msgin/adapter/database/sql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewQueueStore_Construction mirrors TestNewInboxDeduper_Construction: no
// real DB — sql.Open is lazy and NewQueueStore never dials at construction.
// It exercises the shared newAdapterBase validation (nil db, nil dialect,
// invalid table) that NewOutboundAdapter and NewPollingSource already cover
// individually, plus that a valid QueueStore reports EmitsLiveValue false (a
// wire store, [] byte payloads).
func TestNewQueueStore_Construction(t *testing.T) {
	tests := []struct {
		name   string
		assert func(t *testing.T)
	}{
		{
			name: "nil db is ErrNilAdapter",
			assert: func(t *testing.T) {
				_, err := msginsql.NewQueueStore(nil, "q", newFakeDialect())
				require.ErrorIs(t, err, msgin.ErrNilAdapter)
			},
		},
		{
			name: "nil dialect is ErrNilDialect",
			assert: func(t *testing.T) {
				_, err := msginsql.NewQueueStore(openDB(t, fakeDriverName), "q", nil)
				require.ErrorIs(t, err, msginsql.ErrNilDialect)
			},
		},
		{
			name: "empty table is ErrInvalidTableName",
			assert: func(t *testing.T) {
				_, err := msginsql.NewQueueStore(openDB(t, fakeDriverName), "", newFakeDialect())
				require.ErrorIs(t, err, msginsql.ErrInvalidTableName)
			},
		},
		{
			name: "valid args construct; EmitsLiveValue is false (wire store)",
			assert: func(t *testing.T) {
				s, err := msginsql.NewQueueStore(openDB(t, fakeDriverName), "q", newFakeDialect())
				require.NoError(t, err)
				require.False(t, s.EmitsLiveValue())
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { tt.assert(t) })
	}
}

// TestNewQueueStore_NilOptionElement proves a nil ELEMENT of opts is a bare
// ErrNilFunc naming the computed 0-based index (Spec 015 §3.1, family R1) rather
// than a panic — AND that the position names sql.NewQueueStore.
//
// NewQueueStore is the first DELEGATOR of Plan 028 (Spec 015 §3.4): it forwards
// opts... to BOTH NewOutboundAdapter and NewPollingSource, so without its own
// standalone pre-check the fault would surface from the delegate and the caller
// would be told "sql.NewOutboundAdapter: nil option at index 0" for a function
// they never called. Every case below therefore asserts the FULL position
// string; an errors.Is-only assertion would pass with the pre-check deleted,
// because the delegate returns an ErrNilFunc of its own (ADR 0031 D-R).
func TestNewQueueStore_NilOptionElement(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		nilDB  bool
		opts   []msginsql.Option
		assert func(t *testing.T, err error)
	}{
		{
			// AC-1 (no panic) + AC-5 (the R1 wrap is BARE) + D-R (the position
			// names the DELEGATOR, not either delegate).
			name: "nil element alone names the delegator, not the delegate",
			opts: []msginsql.Option{nil},
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrNilFunc)
				assert.False(t, msgin.IsPermanent(err),
					"R1 nil-option error must be BARE, not Permanent-wrapped")
				assert.Contains(t, err.Error(), "sql.NewQueueStore: nil option at index 0")
				assert.NotContains(t, err.Error(), "NewOutboundAdapter",
					"the caller never called NewOutboundAdapter (ADR 0031 D-R)")
				assert.NotContains(t, err.Error(), "NewPollingSource",
					"the caller never called NewPollingSource (ADR 0031 D-R)")
			},
		},
		{
			// AC-2: computed index, asserted as the FULL position string.
			name: "nil element after a valid option reports the computed index",
			opts: []msginsql.Option{msginsql.WithLockedBy("worker-1"), nil},
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrNilFunc)
				assert.Contains(t, err.Error(), "sql.NewQueueStore: nil option at index 1")
			},
		},
		{
			// AC-3: first-nil-wins.
			name: "first of two nils wins",
			opts: []msginsql.Option{nil, nil},
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrNilFunc)
				assert.Contains(t, err.Error(), "sql.NewQueueStore: nil option at index 0")
			},
		},
		{
			// Precedence (Spec 015 §3.5): the pre-check sits at the TOP of the
			// function, above the first delegate call, so it beats every
			// argument fault the delegates would report.
			name:  "the pre-check precedes the delegates' nil-db validation",
			nilDB: true,
			opts:  []msginsql.Option{nil},
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrNilFunc)
				assert.NotErrorIs(t, err, msgin.ErrNilAdapter)
				assert.Contains(t, err.Error(), "sql.NewQueueStore: nil option at index 0")
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var db *sql.DB
			if !tc.nilDB {
				db = openDB(t, fakeDriverName)
			}
			_, err := msginsql.NewQueueStore(db, "q", newFakeDialect(), tc.opts...)
			tc.assert(t, err)
		})
	}
}
