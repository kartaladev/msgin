package mysql_test

// Fast unit tests (no container, no test-deps beyond stdlib) carried forward
// in spirit from the root mysql_test.go (Plan 006 Task 5): the two branches
// that need no real database — the transactional-Querier guard (a) and the
// reference-DDL text (c) — stay here so the mysql module keeps its own
// coverage for the hot-path/typed-error branches its Claim/DDL/InboxDDL
// introduce, without pulling testify (or a driver/testcontainers) into this
// leaf-test module's go.mod (it requires the engine ONLY). Assertions are
// therefore plain stdlib comparisons, not testify, but still follow the
// table-test assert-closure shape. (The remaining carry-forward item,
// TestMySQLClaimInExistingTransaction (b), needs a real MySQL/MariaDB
// container and lives in dbtest's conformance suite.)

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	msginsql "github.com/kartaladev/msgin/adapter/database/sql"
	"github.com/kartaladev/msgin/adapter/database/sql/mysql"
)

// nonTxQuerier is a msginsql.Querier that is NEITHER a *sql.DB nor a *sql.Tx, so
// the MySQL dialect's Claim cannot obtain a transaction from it. Its methods are
// never actually reached — Claim rejects the type before issuing any query — so
// they may return zero values.
type nonTxQuerier struct{}

func (nonTxQuerier) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, nil
}
func (nonTxQuerier) QueryContext(context.Context, string, ...any) (*sql.Rows, error) {
	return nil, nil
}
func (nonTxQuerier) QueryRowContext(context.Context, string, ...any) *sql.Row {
	return nil
}

// TestMySQLClaimRequiresTransactionalQuerier pins the defensive branch of the
// MySQL two-step claim: given a Querier that can neither begin a transaction
// (*sql.DB) nor already be one (*sql.Tx), Claim returns a clear error rather than
// running a non-atomic (double-claim-prone) two-step. No database is needed — the
// type check precedes any query.
func TestMySQLClaimRequiresTransactionalQuerier(t *testing.T) {
	t.Parallel()

	rows, err := mysql.LeaseDialect().Claim(t.Context(), nonTxQuerier{}, "msgs", 1, "worker", time.Minute)
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if rows != nil {
		t.Errorf("expected nil rows, got %v", rows)
	}
	if !strings.Contains(err.Error(), "*sql.DB or *sql.Tx") {
		t.Errorf("error %q must explain an atomic (transactional) Querier is required", err.Error())
	}
}

// TestMySQLDDLIdentifierValidation mirrors the postgres module's DDL text
// expectations for the MySQL reference DDL: a valid identifier produces a
// backtick-quoted CREATE TABLE with the inline claim index; a bad identifier is
// rejected (ErrInvalidTableName) before any string is built.
func TestMySQLDDLIdentifierValidation(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name   string
		table  string
		assert func(t *testing.T, ddl string, err error)
	}

	cases := []testCase{
		{
			name:  "valid identifier produces DDL",
			table: "msg_queue",
			assert: func(t *testing.T, ddl string, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				for _, want := range []string{
					"CREATE TABLE IF NOT EXISTS `msg_queue`",
					"`msg_queue_claim_idx`",
					"AUTO_INCREMENT",
				} {
					if !strings.Contains(ddl, want) {
						t.Errorf("ddl must contain %q; got:\n%s", want, ddl)
					}
				}
			},
		},
		{
			name:  "leading digit rejected",
			table: "1bad",
			assert: func(t *testing.T, ddl string, err error) {
				if !errors.Is(err, msginsql.ErrInvalidTableName) {
					t.Fatalf("expected ErrInvalidTableName, got %v", err)
				}
				if ddl != "" {
					t.Errorf("expected empty ddl on error, got %q", ddl)
				}
			},
		},
		{
			name:  "sql injection attempt rejected",
			table: "t`; DROP TABLE users; --",
			assert: func(t *testing.T, ddl string, err error) {
				if !errors.Is(err, msginsql.ErrInvalidTableName) {
					t.Fatalf("expected ErrInvalidTableName, got %v", err)
				}
				if ddl != "" {
					t.Errorf("expected empty ddl on error, got %q", ddl)
				}
			},
		},
		{
			name:  "empty name rejected",
			table: "",
			assert: func(t *testing.T, ddl string, err error) {
				if !errors.Is(err, msginsql.ErrInvalidTableName) {
					t.Fatalf("expected ErrInvalidTableName, got %v", err)
				}
				if ddl != "" {
					t.Errorf("expected empty ddl on error, got %q", ddl)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ddl, err := mysql.DDL(tc.table)
			tc.assert(t, ddl, err)
		})
	}
}

// TestMySQLInboxDDLIdentifierValidation covers InboxDDL's own text +
// validation guard, the inbox peer of TestMySQLDDLIdentifierValidation. It was
// not present in the pre-split root mysql_test.go (which only exercised the
// lease DDL) but is added here for parity, since InboxDDL is a distinct public
// entry point with its own ValidateIdent call site (Plan 006 Task 5
// carry-forward item (c)).
func TestMySQLInboxDDLIdentifierValidation(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name   string
		table  string
		assert func(t *testing.T, ddl string, err error)
	}

	cases := []testCase{
		{
			name:  "valid identifier produces DDL",
			table: "msgin_inbox",
			assert: func(t *testing.T, ddl string, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				for _, want := range []string{
					"CREATE TABLE IF NOT EXISTS `msgin_inbox`",
					"msg_id",
					"processed_at",
				} {
					if !strings.Contains(ddl, want) {
						t.Errorf("ddl must contain %q; got:\n%s", want, ddl)
					}
				}
			},
		},
		{
			name:  "invalid identifier is rejected before building any SQL",
			table: "bad table; DROP",
			assert: func(t *testing.T, ddl string, err error) {
				if !errors.Is(err, msginsql.ErrInvalidTableName) {
					t.Fatalf("expected ErrInvalidTableName, got %v", err)
				}
				if ddl != "" {
					t.Errorf("expected empty ddl on error, got %q", ddl)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ddl, err := mysql.InboxDDL(tc.table)
			tc.assert(t, ddl, err)
		})
	}
}
