package stdlib_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	msgin "github.com/kartaladev/msgin"
	msghttp "github.com/kartaladev/msgin/adapter/http"
	"github.com/kartaladev/msgin/adapter/http/stdlib"
)

// TestNilOptionElement proves that a nil ELEMENT of opts at either of this
// package's two entry points is a bare [msgin.ErrNilFunc] naming the
// constructor the CALLER invoked (Spec 015 §3.1, family R1) rather than the
// laundered "msghttp.NewConfig" position the underlying delegate would report.
//
// Both NewInbound and NewInboundGateway forward opts to msghttp.NewConfig as
// their first statement (Task 6 already guards that entry point on its own
// behalf, naming ITSELF). Without a standalone pre-check here, a nil element
// handed to stdlib.NewInbound would surface "msghttp.NewConfig: nil option at
// index 0" — a function the caller never called (ADR 0031 D-R). So every case
// below asserts the FULL position string and that it does NOT name NewConfig.
func TestNilOptionElement(t *testing.T) {
	t.Parallel()

	// realOpt is a valid, always-accepted option used to push the nil to a
	// non-zero index for the AC-2 cases. WithFollowRedirects never fails
	// NewConfig's validation, so it isolates the nil-option branch.
	realOpt := msghttp.WithFollowRedirects()

	tests := []struct {
		name   string
		call   func(opts []msghttp.Option) error
		opts   []msghttp.Option
		assert func(t *testing.T, err error)
	}{
		// -----------------------------------------------------------
		// stdlib.NewInbound — DELEGATOR (target validated AFTER
		// msghttp.NewConfig).
		// -----------------------------------------------------------
		{
			// AC-1 (no panic) + AC-5 (the R1 wrap is BARE, never Permanent).
			name: "NewInbound: nil element alone names the delegator",
			call: func(opts []msghttp.Option) error {
				_, err := stdlib.NewInbound(acceptingTarget(t), opts...)
				return err
			},
			opts: []msghttp.Option{nil},
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrNilFunc)
				assert.False(t, msgin.IsPermanent(err),
					"R1 nil-option error must be BARE, not Permanent-wrapped")
				assert.Contains(t, err.Error(), "stdlib.NewInbound: nil option at index 0")
				assert.NotContains(t, err.Error(), "NewConfig",
					"the caller never called msghttp.NewConfig (ADR 0031 D-R)")
			},
		},
		{
			// AC-2: computed index, asserted as the FULL position string.
			name: "NewInbound: nil element after a valid option",
			call: func(opts []msghttp.Option) error {
				_, err := stdlib.NewInbound(acceptingTarget(t), opts...)
				return err
			},
			opts: []msghttp.Option{realOpt, nil},
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrNilFunc)
				assert.Contains(t, err.Error(), "stdlib.NewInbound: nil option at index 1")
			},
		},
		{
			// AC-3: first-nil-wins — one fault, not a list.
			name: "NewInbound: first of two nils wins",
			call: func(opts []msghttp.Option) error {
				_, err := stdlib.NewInbound(acceptingTarget(t), opts...)
				return err
			},
			opts: []msghttp.Option{nil, nil},
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrNilFunc)
				assert.Contains(t, err.Error(), "stdlib.NewInbound: nil option at index 0")
			},
		},
		{
			// Precedence: the pre-check is above msghttp.NewConfig, and
			// target is validated only after that returns, so a nil opt
			// beats msghttp.ErrNilTarget even when target is ALSO nil.
			name: "NewInbound: the nil-option check runs first and wins over ErrNilTarget",
			call: func(opts []msghttp.Option) error {
				_, err := stdlib.NewInbound(nil, opts...)
				return err
			},
			opts: []msghttp.Option{nil},
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrNilFunc)
				assert.NotErrorIs(t, err, msghttp.ErrNilTarget)
				assert.Contains(t, err.Error(), "stdlib.NewInbound: nil option at index 0")
			},
		},

		// -----------------------------------------------------------
		// stdlib.NewInboundGateway — DELEGATOR (exchange validated
		// AFTER msghttp.NewConfig).
		// -----------------------------------------------------------
		{
			name: "NewInboundGateway: nil element alone names the delegator",
			call: func(opts []msghttp.Option) error {
				_, err := stdlib.NewInboundGateway(echoExchange(t), opts...)
				return err
			},
			opts: []msghttp.Option{nil},
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrNilFunc)
				assert.False(t, msgin.IsPermanent(err),
					"R1 nil-option error must be BARE, not Permanent-wrapped")
				assert.Contains(t, err.Error(), "stdlib.NewInboundGateway: nil option at index 0")
				assert.NotContains(t, err.Error(), "NewConfig",
					"the caller never called msghttp.NewConfig (ADR 0031 D-R)")
			},
		},
		{
			name: "NewInboundGateway: nil element after a valid option",
			call: func(opts []msghttp.Option) error {
				_, err := stdlib.NewInboundGateway(echoExchange(t), opts...)
				return err
			},
			opts: []msghttp.Option{realOpt, nil},
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrNilFunc)
				assert.Contains(t, err.Error(), "stdlib.NewInboundGateway: nil option at index 1")
			},
		},
		{
			name: "NewInboundGateway: first of two nils wins",
			call: func(opts []msghttp.Option) error {
				_, err := stdlib.NewInboundGateway(echoExchange(t), opts...)
				return err
			},
			opts: []msghttp.Option{nil, nil},
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrNilFunc)
				assert.Contains(t, err.Error(), "stdlib.NewInboundGateway: nil option at index 0")
			},
		},
		{
			// Precedence: the pre-check runs before exchange is checked, so
			// a nil opt beats msgin.ErrNilExchange even when exchange is
			// ALSO nil.
			name: "NewInboundGateway: the nil-option check runs first and wins over ErrNilExchange",
			call: func(opts []msghttp.Option) error {
				_, err := stdlib.NewInboundGateway(nil, opts...)
				return err
			},
			opts: []msghttp.Option{nil},
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrNilFunc)
				assert.NotErrorIs(t, err, msgin.ErrNilExchange)
				assert.Contains(t, err.Error(), "stdlib.NewInboundGateway: nil option at index 0")
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tc.assert(t, tc.call(tc.opts))
		})
	}
}

// TestNilOptionElement_DistinctPositions proves the two entry points really do
// produce two DIFFERENT position strings from the single msghttp.Option type
// — the property D-R exists to buy. A regression that dropped either
// constructor's pre-check would collapse that name onto "msghttp.NewConfig"
// and this test would fail on the duplicate, even if every individual
// position assertion above were relaxed to an errors.Is check.
func TestNilOptionElement_DistinctPositions(t *testing.T) {
	t.Parallel()

	calls := map[string]func() error{
		"stdlib.NewInbound": func() error {
			_, err := stdlib.NewInbound(acceptingTarget(t), nil)
			return err
		},
		"stdlib.NewInboundGateway": func() error {
			_, err := stdlib.NewInboundGateway(echoExchange(t), nil)
			return err
		},
	}

	seen := make(map[string]string, len(calls))
	for ctor, call := range calls {
		err := call()
		require.ErrorIs(t, err, msgin.ErrNilFunc, "%s", ctor)
		msg := err.Error()
		// assert, NOT require: a require here would abort the loop on the
		// first collapsed position and the duplicate branch below would
		// never run — a dead branch that proves nothing.
		assert.Contains(t, msg, ctor+": nil option at index 0")
		if prev, dup := seen[msg]; dup {
			t.Errorf("%s and %s produce the SAME position %q — a pre-check is missing", prev, ctor, msg)
		}
		seen[msg] = ctor
	}
	assert.Len(t, seen, len(calls), "each entry point must produce its own position string")
}
