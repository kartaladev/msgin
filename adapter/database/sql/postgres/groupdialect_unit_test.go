package postgres_test

// D-AV's SPI-boundary bound check, exercised with NO database and NO test
// dependency beyond the stdlib — the property this leaf module's existing unit
// tests are written to preserve (see dialect_test.go's header in the mysql
// module: "no test-deps beyond stdlib", assertions are plain stdlib comparisons
// that still follow the table-test assert-closure shape).

import (
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	msgin "github.com/kartaladev/msgin"
	msginsql "github.com/kartaladev/msgin/adapter/database/sql"
	"github.com/kartaladev/msgin/adapter/database/sql/postgres"
)

// TestGroupDialectAddMemberValidatesMaxMembers proves this dialect enforces
// ADR 0033 D-AV's SPI-boundary contract (Spec 017 §3.6a.2, whole-branch review
// finding R-15 — a CLAUDE.md delivery blocker): AddMember rejects any
// maxMembers outside {msginsql.UnboundedGroupMembers} u [1, 1048576] with
// msgin.ErrInvalidCapacity, BEFORE any statement runs.
//
// Every row passes a NIL Querier, which is what makes the placement assertion
// real rather than nominal: a reject row that reached I/O would surface this
// dialect's "group ops require a ..." Querier error instead of
// ErrInvalidCapacity, and an accept row PROVES the validator let the value
// through precisely BECAUSE that Querier error is what comes back.
func TestGroupDialectAddMemberValidatesMaxMembers(t *testing.T) {
	t.Parallel()

	const leaseTTL = time.Minute
	const querierRefusal = "group ops require"

	// rejected asserts the D-AV arm: a typed msgin.ErrInvalidCapacity naming
	// this engine, with no I/O attempted.
	rejected := func(t *testing.T, err error) {
		t.Helper()
		if !errors.Is(err, msgin.ErrInvalidCapacity) {
			t.Fatalf("want msgin.ErrInvalidCapacity, got %v", err)
		}
		if !msgin.IsPermanent(err) {
			t.Errorf("an invalid constant bound never becomes valid on redelivery (B-1); "+
				"want Permanent, got %v", err)
		}
		if want := "msgin/sql/postgres: AddMember"; !strings.Contains(err.Error(), want) {
			t.Errorf("the site must name the ENGINE driven with the bad bound: want %q in %q", want, err)
		}
		if strings.Contains(err.Error(), querierRefusal) {
			t.Errorf("validation MUST precede any I/O; the nil Querier was reached: %v", err)
		}
	}
	// accepted asserts the value passed the validator: the ONLY way to observe
	// that without a database is that the dialect then tried to use the nil
	// Querier and complained about IT.
	accepted := func(t *testing.T, err error) {
		t.Helper()
		if err == nil {
			t.Fatal("a nil Querier cannot succeed")
		}
		if errors.Is(err, msgin.ErrInvalidCapacity) {
			t.Fatalf("this bound is inside the accepted set and must not be refused: %v", err)
		}
		if !strings.Contains(err.Error(), querierRefusal) {
			t.Errorf("want the dialect to get past validation and reach the Querier, got %v", err)
		}
	}

	tests := []struct {
		name       string
		maxMembers int
		assert     func(t *testing.T, err error)
	}{
		{"UnboundedGroupMembers is accepted", msginsql.UnboundedGroupMembers, accepted},
		{"the lower bound 1 is accepted", 1, accepted},
		{"the ceiling 1<<20 is accepted", 1 << 20, accepted},
		{"zero is rejected - no longer a synonym for unbounded", 0, rejected},
		{"a non-sentinel negative is rejected", -2, rejected},
		{"one past the ceiling is rejected", (1 << 20) + 1, rejected},
		{"math.MaxInt is rejected - no count can exceed it, so the cap never fires (R-15)", math.MaxInt, rejected},
		{"math.MinInt is rejected", math.MinInt, rejected},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := postgres.GroupDialect().AddMember(t.Context(), nil,
				"msgin_group", "k", "m-1", 0, nil, nil, tt.maxMembers, leaseTTL)
			tt.assert(t, err)
		})
	}
}

// TestGroupDialectAddMemberRejectsEmptyMsgIDBeforeTheBound pins the ORDER of
// the two validate-before-I/O checks. Both satisfy ADR 0033 D-AV's "the same
// validate-before-I/O placement ErrMissingMsgID already has"; the shipped msgID
// check stays FIRST so an id-less member keeps its pre-existing diagnosis
// rather than being re-reported as a capacity fault.
func TestGroupDialectAddMemberRejectsEmptyMsgIDBeforeTheBound(t *testing.T) {
	t.Parallel()
	_, err := postgres.GroupDialect().AddMember(t.Context(), nil,
		"msgin_group", "k", "", 0, nil, nil, 0, time.Minute)
	if !errors.Is(err, msginsql.ErrMissingMsgID) {
		t.Fatalf("want msginsql.ErrMissingMsgID, got %v", err)
	}
	if errors.Is(err, msgin.ErrInvalidCapacity) {
		t.Errorf("the msgID check runs first; it must not be superseded by the bound check: %v", err)
	}
}
