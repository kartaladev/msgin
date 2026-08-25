package sql_test

import (
	"math"
	"testing"

	msgin "github.com/kartaladev/msgin"
	msginsql "github.com/kartaladev/msgin/adapter/database/sql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestUnboundedGroupMembers pins the sentinel's VALUE, not merely its
// existence: D-AV chose -1 precisely so the ZERO value is not the dangerous
// one, and a silent redefinition to 0 would restore the fail-open contract the
// decision exists to close (Spec 017 §3.6a.2).
func TestUnboundedGroupMembers(t *testing.T) {
	t.Parallel()
	assert.Equal(t, -1, msginsql.UnboundedGroupMembers,
		"the unbounded sentinel MUST NOT be the zero value (ADR 0033 D-AV)")
}

// TestValidateMaxMembers covers every arm of the SPI-boundary check D-AV adds
// to GroupDialect.AddMember (Spec 017 §3.6a.2, whole-branch review finding
// R-15 — a CLAUDE.md delivery blocker).
//
// The accepted set is {UnboundedGroupMembers} u [1, 1048576]; every other int
// is a typed msgin.ErrInvalidCapacity. The two rows that matter most are `0`
// (previously a silent synonym for UNBOUNDED — the zero value was the
// dangerous value) and math.MaxInt (previously accepted, and then wrapped
// selectLimit to math.MinInt, suppressing BOTH the LIMIT clause and the cap
// comparison, so the largest expressible bound silently meant *no* bound).
func TestValidateMaxMembers(t *testing.T) {
	t.Parallel()

	const site = "msgin/sql/postgres: AddMember"

	tests := []struct {
		name       string
		maxMembers int
		assert     func(t *testing.T, err error)
	}{
		{
			name:       "UnboundedGroupMembers is the ONLY accepted non-positive value",
			maxMembers: msginsql.UnboundedGroupMembers,
			assert: func(t *testing.T, err error) {
				require.NoError(t, err, "the documented opt-out must stay reachable")
			},
		},
		{
			name:       "the lower bound 1 is accepted",
			maxMembers: 1,
			assert: func(t *testing.T, err error) {
				require.NoError(t, err)
			},
		},
		{
			name:       "the ceiling 1<<20 is accepted",
			maxMembers: 1 << 20,
			assert: func(t *testing.T, err error) {
				require.NoError(t, err, "the ceiling itself is IN range, not one past it")
			},
		},
		{
			name:       "zero is REJECTED — it is no longer a synonym for unbounded",
			maxMembers: 0,
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrInvalidCapacity)
				assert.True(t, msgin.IsPermanent(err),
					"a constant argument never becomes valid on redelivery; a transient "+
						"classification here hot-spins under the zero-value RetryPolicy (B-1)")
				assert.EqualError(t, err,
					"msgin: permanent: msgin: capacity out of range: msgin/sql/postgres: AddMember: "+
						"0 not in [1, 1048576] and not UnboundedGroupMembers (-1)")
			},
		},
		{
			name:       "a non-sentinel negative is REJECTED",
			maxMembers: -2,
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrInvalidCapacity)
				assert.EqualError(t, err,
					"msgin: permanent: msgin: capacity out of range: msgin/sql/postgres: AddMember: "+
						"-2 not in [1, 1048576] and not UnboundedGroupMembers (-1)")
			},
		},
		{
			name:       "one past the ceiling is REJECTED",
			maxMembers: (1 << 20) + 1,
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrInvalidCapacity)
				assert.EqualError(t, err,
					"msgin: permanent: msgin: capacity out of range: msgin/sql/postgres: AddMember: "+
						"1048577 not in [1, 1048576] and not UnboundedGroupMembers (-1)")
			},
		},
		{
			// R-15's second half. No render assertion: math.MaxInt's decimal
			// is GOARCH-dependent, and the class gate's header records why a
			// decimal string must never ride on an arch-dependent literal.
			name:       "math.MaxInt is REJECTED — the selectLimit overflow (R-15)",
			maxMembers: math.MaxInt,
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrInvalidCapacity)
			},
		},
		{
			name:       "math.MinInt is REJECTED",
			maxMembers: math.MinInt,
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrInvalidCapacity)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tt.assert(t, msginsql.ValidateMaxMembers(site, tt.maxMembers))
		})
	}
}
