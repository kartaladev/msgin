package sql_test

import (
	"database/sql"
	"testing"
	"time"

	msgin "github.com/kartaladev/msgin"
	msginsql "github.com/kartaladev/msgin/adapter/database/sql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewOutboundAdapter_Construction mirrors TestNewPollingSource_Construction:
// NewOutboundAdapter shares the same construction-time validation via the
// shared adapterBase, over the explicit-dialect API (ADR 0011). No database
// connection is made — sql.Open is lazy and construction never dials.
func TestNewOutboundAdapter_Construction(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name    string
		db      func(t *testing.T) *sql.DB // nil => pass a nil *sql.DB
		table   string
		dialect msginsql.LeaseDialect
		opts    []msginsql.Option
		assert  func(t *testing.T, out *msginsql.Outbound, err error)
	}

	cases := []testCase{
		{
			name:    "nil db is ErrNilAdapter",
			db:      nil,
			table:   "msgs",
			dialect: newFakeDialect(),
			assert: func(t *testing.T, out *msginsql.Outbound, err error) {
				require.ErrorIs(t, err, msgin.ErrNilAdapter)
				assert.Nil(t, out)
			},
		},
		{
			name:    "invalid table name is rejected before touching the db",
			db:      func(t *testing.T) *sql.DB { return openDB(t, fakeDriverName) },
			table:   "bad table; DROP",
			dialect: newFakeDialect(),
			assert: func(t *testing.T, out *msginsql.Outbound, err error) {
				require.ErrorIs(t, err, msginsql.ErrInvalidTableName)
				assert.Nil(t, out)
			},
		},
		{
			name:    "nil dialect is ErrNilDialect",
			db:      func(t *testing.T) *sql.DB { return openDB(t, fakeDriverName) },
			table:   "msgs",
			dialect: nil,
			assert: func(t *testing.T, out *msginsql.Outbound, err error) {
				require.ErrorIs(t, err, msginsql.ErrNilDialect)
				assert.Nil(t, out)
			},
		},
		{
			name:    "a valid db/table/dialect constructs",
			db:      func(t *testing.T) *sql.DB { return openDB(t, fakeDriverName) },
			table:   "msgs",
			dialect: newFakeDialect(),
			assert: func(t *testing.T, out *msginsql.Outbound, err error) {
				require.NoError(t, err)
				assert.NotNil(t, out)
			},
		},
		{
			name:    "WithLeaseTTL/WithLockedBy (lease-Source-specific) are inert but do not error",
			db:      func(t *testing.T) *sql.DB { return openDB(t, fakeDriverName) },
			table:   "msgs",
			dialect: newFakeDialect(),
			opts:    []msginsql.Option{msginsql.WithLeaseTTL(30 * time.Second), msginsql.WithLockedBy("owner")},
			assert: func(t *testing.T, out *msginsql.Outbound, err error) {
				require.NoError(t, err)
				assert.NotNil(t, out)
			},
		},
		{
			name:    "WithSharedTransaction(nil) is a construction-time ErrNilResolver, not a deferred panic",
			db:      func(t *testing.T) *sql.DB { return openDB(t, fakeDriverName) },
			table:   "msgs",
			dialect: newFakeDialect(),
			opts:    []msginsql.Option{msginsql.WithSharedTransaction(nil)},
			assert: func(t *testing.T, out *msginsql.Outbound, err error) {
				require.ErrorIs(t, err, msginsql.ErrNilResolver)
				assert.Nil(t, out)
			},
		},
		{
			name:    "WithOpportunisticSharedTransaction(nil) is also a construction-time ErrNilResolver",
			db:      func(t *testing.T) *sql.DB { return openDB(t, fakeDriverName) },
			table:   "msgs",
			dialect: newFakeDialect(),
			opts:    []msginsql.Option{msginsql.WithOpportunisticSharedTransaction(nil)},
			assert: func(t *testing.T, out *msginsql.Outbound, err error) {
				require.ErrorIs(t, err, msginsql.ErrNilResolver)
				assert.Nil(t, out)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var db *sql.DB
			if tc.db != nil {
				db = tc.db(t)
			}
			out, err := msginsql.NewOutboundAdapter(db, tc.table, tc.dialect, tc.opts...)
			tc.assert(t, out, err)
		})
	}
}

// TestOutbound_NotLiveValueSource pins the wire-adapter contract: Outbound must
// NOT implement msgin.LiveValueSource, so endpoint.NewProducer always
// JSON-encodes the payload to []byte before calling Send (ADR 0010 D8).
func TestOutbound_NotLiveValueSource(t *testing.T) {
	t.Parallel()

	out, err := msginsql.NewOutboundAdapter(openDB(t, fakeDriverName), "msgs", newFakeDialect())
	require.NoError(t, err)

	_, ok := any(out).(msgin.LiveValueSource)
	assert.False(t, ok, "Outbound must not implement LiveValueSource")
}

// TestNewOutboundAdapter_NilOptionElement proves a nil ELEMENT of opts is a bare
// ErrNilFunc naming the computed 0-based index (Spec 015 §3.1, family R1) rather
// than a panic. NewOutboundAdapter is LOOP-FIRST (Spec 015 §3.5): the apply loop
// is the constructor's first statement, so a nil option beats the ErrNilResolver
// check AND newAdapterBase's db/table/dialect validation, all of which run after
// it.
func TestNewOutboundAdapter_NilOptionElement(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		db      func(t *testing.T) *sql.DB // nil => pass a nil *sql.DB
		table   string
		dialect msginsql.LeaseDialect
		opts    []msginsql.Option
		assert  func(t *testing.T, err error)
	}{
		{
			// AC-1 (no panic) + AC-5 (the R1 wrap is BARE).
			name:    "nil element alone",
			db:      func(t *testing.T) *sql.DB { return openDB(t, fakeDriverName) },
			table:   "msgs",
			dialect: newFakeDialect(),
			opts:    []msginsql.Option{nil},
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrNilFunc)
				assert.False(t, msgin.IsPermanent(err),
					"R1 nil-option error must be BARE, not Permanent-wrapped")
				assert.Contains(t, err.Error(), "sql.NewOutboundAdapter: nil option at index 0")
			},
		},
		{
			// AC-2: the index is computed, and the FULL position (constructor
			// name included) is asserted so a copy-pasted ctor literal dies.
			name:    "nil element after a valid option reports the computed index",
			db:      func(t *testing.T) *sql.DB { return openDB(t, fakeDriverName) },
			table:   "msgs",
			dialect: newFakeDialect(),
			opts:    []msginsql.Option{msginsql.WithLeaseTTL(time.Minute), nil},
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrNilFunc)
				assert.Contains(t, err.Error(), "sql.NewOutboundAdapter: nil option at index 1")
			},
		},
		{
			// AC-3: first-nil-wins — an implementation reporting the LAST nil
			// passes every other assertion in this table.
			name:    "first of two nils wins",
			db:      func(t *testing.T) *sql.DB { return openDB(t, fakeDriverName) },
			table:   "msgs",
			dialect: newFakeDialect(),
			opts:    []msginsql.Option{nil, nil},
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrNilFunc)
				assert.Contains(t, err.Error(), "sql.NewOutboundAdapter: nil option at index 0")
			},
		},
		{
			// Precedence (Spec 015 §3.5): LOOP-FIRST, so the nil option beats
			// newAdapterBase's nil-db check, which runs after the loop.
			name:    "nil option precedes the nil-db validation",
			db:      nil,
			table:   "msgs",
			dialect: newFakeDialect(),
			opts:    []msginsql.Option{nil},
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrNilFunc)
				assert.NotErrorIs(t, err, msgin.ErrNilAdapter)
				assert.Contains(t, err.Error(), "sql.NewOutboundAdapter: nil option at index 0")
			},
		},
		{
			// Precedence: the nil option also beats the ErrNilResolver check,
			// the only validation between the loop and newAdapterBase.
			name:    "nil option precedes the ErrNilResolver validation",
			db:      func(t *testing.T) *sql.DB { return openDB(t, fakeDriverName) },
			table:   "msgs",
			dialect: newFakeDialect(),
			opts:    []msginsql.Option{msginsql.WithSharedTransaction(nil), nil},
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrNilFunc)
				assert.NotErrorIs(t, err, msginsql.ErrNilResolver)
				assert.Contains(t, err.Error(), "sql.NewOutboundAdapter: nil option at index 1")
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var db *sql.DB
			if tc.db != nil {
				db = tc.db(t)
			}
			_, err := msginsql.NewOutboundAdapter(db, tc.table, tc.dialect, tc.opts...)
			tc.assert(t, err)
		})
	}
}
