package sql_test

import (
	"testing"
	"time"

	msginsql "github.com/kartaladev/msgin/adapter/database/sql"
	"github.com/stretchr/testify/require"
)

// TestDialectMethodsRejectInvalidTable verifies every LeaseDialect method (on BOTH
// built-in dialects) validates its table identifier up front and returns
// ErrInvalidTableName before touching the database. Validation is the first
// statement of each method — for MySQL's Claim it precedes even the
// transaction-capability check — so the nil Querier below is never dereferenced:
// the reject branch returns first. This is the typed-error surface the CLAUDE.md
// coverage gate requires to be tested, per dialect.
func TestDialectMethodsRejectInvalidTable(t *testing.T) {
	t.Parallel()

	const badTable = "bad table; DROP" // space + semicolon fail validateIdent

	type testCase struct {
		name string
		call func(d msginsql.LeaseDialect) error
	}

	cases := []testCase{
		{
			name: "Claim",
			call: func(d msginsql.LeaseDialect) error {
				_, err := d.Claim(t.Context(), nil, badTable, 1, "w", time.Minute)
				return err
			},
		},
		{
			name: "Ack",
			call: func(d msginsql.LeaseDialect) error {
				_, err := d.Ack(t.Context(), nil, badTable, 1, "w", 1)
				return err
			},
		},
		{
			name: "Nack",
			call: func(d msginsql.LeaseDialect) error {
				_, err := d.Nack(t.Context(), nil, badTable, 1, "w", 1, time.Second)
				return err
			},
		},
		{
			name: "Insert",
			call: func(d msginsql.LeaseDialect) error {
				return d.Insert(t.Context(), nil, badTable, "m", []byte("{}"), []byte("p"), 0)
			},
		},
		{
			name: "EnsureSchema",
			call: func(d msginsql.LeaseDialect) error {
				return d.EnsureSchema(t.Context(), nil, badTable)
			},
		},
		{
			name: "SchemaExists",
			call: func(d msginsql.LeaseDialect) error {
				_, err := d.SchemaExists(t.Context(), nil, badTable)
				return err
			},
		},
	}

	dialects := []struct {
		name string
		d    msginsql.LeaseDialect
	}{
		{"postgres", msginsql.PostgresDialect()},
		{"mysql", msginsql.MySQLDialect()},
	}

	for _, dl := range dialects {
		for _, tc := range cases {
			t.Run(dl.name+"/"+tc.name, func(t *testing.T) {
				t.Parallel()
				err := tc.call(dl.d)
				require.ErrorIs(t, err, msginsql.ErrInvalidTableName)
			})
		}
	}
}
