package harness_test

// groupstore_test.go is the harness module's own blackbox test. The RunXxx
// conformance functions themselves need a real database and are executed by
// the dbtest runner, but DialectEngine is pure derivation, so the one input
// shape no shipped dialect exercises — a POINTER-typed GroupDialect — is
// provable here without Docker (Plan 031 Task 11, review finding R-12).
//
// All three shipped dialects (postgres, mysql, sqlite) are value types, so
// before this file the member-cap conformance suite would have asserted every
// contributor on pointer receivers against the malformed site string
// "msgin/sql/: AddMember", failing with a diff blaming THEIR error text.

import (
	"context"
	"testing"
	"time"

	msginsql "github.com/kartaladev/msgin/adapter/database/sql"
	"github.com/kartaladev/msgin/adapter/database/sql/harness"
	"github.com/stretchr/testify/assert"
)

// stubGroupDialect is a do-nothing msginsql.GroupDialect that exists only to
// carry a package path into harness.DialectEngine. Its methods take VALUE
// receivers deliberately: that puts them in both stubGroupDialect's and
// *stubGroupDialect's method sets, so one type can be handed to DialectEngine
// in both forms and the two results compared directly.
type stubGroupDialect struct{}

func (stubGroupDialect) AddMember(context.Context, msginsql.Querier, string, string, string, int64, []byte, []byte, int, time.Duration) (msginsql.GroupRows, error) {
	return msginsql.GroupRows{}, nil
}

func (stubGroupDialect) ClaimGroup(context.Context, msginsql.Querier, string, string, string, time.Duration) (*msginsql.ClaimedGroup, error) {
	return nil, nil
}

func (stubGroupDialect) SettleGroup(context.Context, msginsql.Querier, string, string, string, int64) (bool, error) {
	return false, nil
}

func (stubGroupDialect) AbandonGroup(context.Context, msginsql.Querier, string, string, string, int64) (bool, error) {
	return false, nil
}

func (stubGroupDialect) ExpiredGroups(context.Context, msginsql.Querier, string, time.Time, time.Duration, int) ([]msginsql.GroupRows, error) {
	return nil, nil
}

func (stubGroupDialect) EnsureGroupSchema(context.Context, msginsql.Querier, string) error {
	return nil
}

func (stubGroupDialect) SchemaExists(context.Context, msginsql.Querier, string) (bool, error) {
	return false, nil
}

// Compile-time assertions in BOTH the forms GroupDialect's godoc sanctions.
var (
	_ msginsql.GroupDialect = stubGroupDialect{}
	_ msginsql.GroupDialect = (*stubGroupDialect)(nil)
)

// TestDialectEngine pins the engine-token derivation the member-cap
// conformance assertions render into "msgin/sql/<engine>: AddMember".
//
// The token is the last element of the DIALECT IMPLEMENTATION's package path,
// never TestKit.Name — the shipped MariaDB runner sets Name "mariadb" while
// running the mysql dialect, so a Name-keyed derivation renders a site the
// dialect never mints. Here the implementation lives in this external test
// package, so the expected token is "harness_test".
func TestDialectEngine(t *testing.T) {
	t.Parallel()

	const wantEngine = "harness_test"

	type testCase struct {
		name    string
		dialect msginsql.GroupDialect
		assert  func(t *testing.T, engine string)
	}

	cases := []testCase{
		{
			name:    "a value-typed dialect derives its package name",
			dialect: stubGroupDialect{},
			assert: func(t *testing.T, engine string) {
				assert.Equal(t, wantEngine, engine)
			},
		},
		{
			// R-12: reflect.Type.PkgPath is "" for a pointer type, and
			// (*yourDialect)(nil) is the conformance form GroupDialect's own
			// godoc prescribes — so this is the shape a contributor is most
			// likely to hand the harness, and the one nothing in-repo covers.
			name:    "a POINTER-typed dialect derives the same token, not the empty string",
			dialect: &stubGroupDialect{},
			assert: func(t *testing.T, engine string) {
				assert.Equal(t, wantEngine, engine,
					"a pointer dialect must not render the malformed site "+
						`"msgin/sql/: AddMember" (R-12)`)
				assert.NotEmpty(t, engine)
			},
		},
		{
			// A stateless dialect is legitimately usable as a typed nil, and
			// that is literally the expression the godoc shows. Indirecting
			// through reflect.ValueOf would panic here; the derivation must
			// stay on the TYPE.
			name:    "a typed-nil pointer dialect derives the token without panicking",
			dialect: (*stubGroupDialect)(nil),
			assert: func(t *testing.T, engine string) {
				assert.Equal(t, wantEngine, engine)
			},
		},
		{
			// Defensive: a kit built with Group unset reaches the derivation
			// before anything dereferences it, and a reflect panic there
			// would bury the real "TestKit.Group is required" mistake.
			name:    "a nil dialect yields an empty token instead of panicking",
			dialect: nil,
			assert: func(t *testing.T, engine string) {
				assert.Empty(t, engine)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tc.assert(t, harness.DialectEngine(tc.dialect))
		})
	}
}

// TestDialectEngine_PointerAndValueAgree states the invariant the per-case
// literals above only imply: ONE dialect type handed to DialectEngine in its
// value and pointer forms must derive the SAME engine token. It survives this
// file moving package, which the "harness_test" literals do not.
func TestDialectEngine_PointerAndValueAgree(t *testing.T) {
	t.Parallel()

	assert.Equal(t,
		harness.DialectEngine(stubGroupDialect{}),
		harness.DialectEngine(&stubGroupDialect{}),
		"the engine token must not depend on whether the dialect is addressed by value or pointer")
}
