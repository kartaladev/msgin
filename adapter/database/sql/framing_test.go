package sql_test

import (
	"testing"
	"time"

	msgin "github.com/kartaladev/msgin"
	msginsql "github.com/kartaladev/msgin/adapter/database/sql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEncodeDecodeHeadersRoundTrip(t *testing.T) {
	t.Parallel()

	ts := time.Date(2026, 7, 17, 12, 34, 56, 123456789, time.UTC)

	type testCase struct {
		name    string
		headers msgin.Headers
		assert  func(t *testing.T, got msgin.Headers, encErr, decErr error)
	}

	cases := []testCase{
		{
			name: "reserved and custom headers round-trip with correct types",
			headers: msgin.NewHeaders(map[string]any{
				msgin.HeaderID:            "abc123",
				msgin.HeaderTimestamp:     ts,
				msgin.HeaderDeliveryCount: 3,
				msgin.HeaderContentType:   "application/json",
				msgin.HeaderCorrelationID: "corr-9",
				"x-custom":                "trace-42",
			}),
			assert: func(t *testing.T, got msgin.Headers, encErr, decErr error) {
				require.NoError(t, encErr)
				require.NoError(t, decErr)

				id, ok := got.String(msgin.HeaderID)
				assert.True(t, ok)
				assert.Equal(t, "abc123", id)

				gotTS, ok := got.Time(msgin.HeaderTimestamp)
				assert.True(t, ok, "timestamp must decode back to time.Time")
				assert.True(t, ts.Equal(gotTS), "timestamp instant preserved: want %v got %v", ts, gotTS)

				dc, ok := got.Int(msgin.HeaderDeliveryCount)
				assert.True(t, ok, "delivery-count must decode back to int")
				assert.Equal(t, 3, dc)

				ct, ok := got.String(msgin.HeaderContentType)
				assert.True(t, ok)
				assert.Equal(t, "application/json", ct)

				corr, ok := got.String(msgin.HeaderCorrelationID)
				assert.True(t, ok)
				assert.Equal(t, "corr-9", corr)

				custom, ok := got.String("x-custom")
				assert.True(t, ok)
				assert.Equal(t, "trace-42", custom)
			},
		},
		{
			name:    "empty headers round-trip",
			headers: msgin.NewHeaders(nil),
			assert: func(t *testing.T, got msgin.Headers, encErr, decErr error) {
				require.NoError(t, encErr)
				require.NoError(t, decErr)
				n := 0
				for range got.All() {
					n++
				}
				assert.Equal(t, 0, n)
			},
		},
		{
			name: "non-encodable custom value surfaces an encode error",
			headers: msgin.NewHeaders(map[string]any{
				"bad": make(chan int),
			}),
			assert: func(t *testing.T, _ msgin.Headers, encErr, _ error) {
				require.Error(t, encErr)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			b, encErr := msginsql.EncodeHeaders(tc.headers)
			if encErr != nil {
				tc.assert(t, msgin.Headers{}, encErr, nil)
				return
			}
			got, decErr := msginsql.DecodeHeaders(b)
			tc.assert(t, got, encErr, decErr)
		})
	}
}

func TestDecodeHeadersMalformedJSON(t *testing.T) {
	t.Parallel()

	_, err := msginsql.DecodeHeaders([]byte("{not json"))
	require.Error(t, err)
}

// TestDecodeHeadersEmptyInput covers DecodeHeaders' zero-length fast path
// (empty Headers, no JSON parse attempted). EncodeHeaders always emits at
// least "{}" (never zero bytes), so the round-trip table above can never
// exercise this branch — it is reachable only via a raw nil/empty []byte, as
// an inbound adapter would see for a row with no framed headers.
func TestDecodeHeadersEmptyInput(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name string
		in   []byte
	}
	cases := []testCase{
		{name: "nil", in: nil},
		{name: "empty slice", in: []byte{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := msginsql.DecodeHeaders(tc.in)
			require.NoError(t, err)
			n := 0
			for range got.All() {
				n++
			}
			assert.Equal(t, 0, n, "empty input must yield empty Headers")
		})
	}
}

// Reference-DDL identifier validation now lives in the postgres and mysql
// leaf-test dialect modules and is exercised via the dbtest harness run (Plan
// 006 Tasks 4-5, harness RunDialect InvalidIdentifierRejected). ValidateIdent's
// exhaustive reject matrix (leading digit, injection, empty) is covered
// directly against the engine's shared validator by the mysql module's own
// TestMySQLDDLIdentifierValidation/TestMySQLInboxDDLIdentifierValidation
// (adapter/database/sql/mysql/dialect_test.go), which drive it through
// mysql.DDL/mysql.InboxDDL — no built-in dialect remains in root to test this
// through.
