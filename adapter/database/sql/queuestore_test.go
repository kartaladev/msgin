package sql_test

import (
	"testing"

	msgin "github.com/kartaladev/msgin"
	msginsql "github.com/kartaladev/msgin/adapter/database/sql"
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
