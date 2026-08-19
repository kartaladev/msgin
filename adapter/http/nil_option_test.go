package msghttp_test

import (
	"strings"
	"testing"

	msgin "github.com/kartaladev/msgin"
	msghttp "github.com/kartaladev/msgin/adapter/http"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNilOptionElement proves that a nil ELEMENT of opts is a bare
// [msgin.ErrNilFunc] naming the constructor the CALLER invoked plus the
// element's 0-based index (Spec 015 §3.1, family R1) rather than the panic an
// unguarded apply loop would raise — at all SIX of this package's entry points.
//
// msghttp is the plan's most delegator-heavy package: ONE Option type
// (options.go:407) feeds one folded guard (NewConfig, which actually applies the
// options) and FIVE delegators (NewExchange, NewOutbound, NewSSEServer,
// NewSSEClient, NewSSEParser), each of which calls NewConfig(opts...) as its
// first statement. So one option type must yield SIX DISTINCT position strings.
//
// Every case therefore asserts the FULL position string, and each delegator case
// additionally asserts the message does NOT name msghttp.NewConfig. An
// errors.Is-only assertion would be worthless here: with a delegator's pre-check
// deleted the nil still reaches NewConfig, which returns an ErrNilFunc of its own
// — so only the position assertion can distinguish a truthful position from one
// naming a function the caller never called (Spec 015 §3.4, ADR 0031 D-R).
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
		// ---------------------------------------------------------------
		// msghttp.NewConfig — the FOLDED guard (the constructor that
		// actually applies the options). Spec 015 §3.5 classifies it
		// LOOP-FIRST: `cfg := &Config{}` then the apply loop is its first
		// statement, so every value check below the loop
		// (ErrInvalidMaxBodyBytes, ErrInvalidStatusCode, …) is unreachable
		// once a nil element is found.
		// ---------------------------------------------------------------
		{
			// AC-1 (no panic) + AC-5 (the R1 wrap is BARE, never Permanent).
			name: "NewConfig: nil element alone",
			call: func(opts []msghttp.Option) error { _, err := msghttp.NewConfig(opts...); return err },
			opts: []msghttp.Option{nil},
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrNilFunc)
				assert.False(t, msgin.IsPermanent(err),
					"R1 nil-option error must be BARE, not Permanent-wrapped")
				assert.Contains(t, err.Error(), "msghttp.NewConfig: nil option at index 0")
			},
		},
		{
			// AC-2: computed index, asserted as the FULL position string.
			name: "NewConfig: nil element after a valid option",
			call: func(opts []msghttp.Option) error { _, err := msghttp.NewConfig(opts...); return err },
			opts: []msghttp.Option{realOpt, nil},
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrNilFunc)
				assert.Contains(t, err.Error(), "msghttp.NewConfig: nil option at index 1")
			},
		},
		{
			// AC-3: first-nil-wins — one fault, not a list.
			name: "NewConfig: first of two nils wins",
			call: func(opts []msghttp.Option) error { _, err := msghttp.NewConfig(opts...); return err },
			opts: []msghttp.Option{nil, nil},
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrNilFunc)
				assert.Contains(t, err.Error(), "msghttp.NewConfig: nil option at index 0")
			},
		},
		{
			// Precedence (Spec 015 §3.5), NewConfig is LOOP-FIRST: the nil
			// option is found inside the apply loop, so WithMaxBodyBytes(0)'s
			// ErrInvalidMaxBodyBytes — validated AFTER the loop — is never
			// reached, even though that option sits at index 0.
			name: "NewConfig: the nil element beats a post-loop value check",
			call: func(opts []msghttp.Option) error { _, err := msghttp.NewConfig(opts...); return err },
			opts: []msghttp.Option{msghttp.WithMaxBodyBytes(0), nil},
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrNilFunc)
				assert.NotErrorIs(t, err, msghttp.ErrInvalidMaxBodyBytes)
				assert.Contains(t, err.Error(), "msghttp.NewConfig: nil option at index 1")
			},
		},

		// ---------------------------------------------------------------
		// msghttp.NewExchange — DELEGATOR (exchange.go: NewConfig(opts...)
		// is its first statement, validateURL second).
		// ---------------------------------------------------------------
		{
			name: "NewExchange: nil element alone names the delegator",
			call: func(opts []msghttp.Option) error {
				_, err := msghttp.NewExchange("http://example.test/hook", opts...)
				return err
			},
			opts: []msghttp.Option{nil},
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrNilFunc)
				assert.False(t, msgin.IsPermanent(err),
					"R1 nil-option error must be BARE, not Permanent-wrapped")
				assert.Contains(t, err.Error(), "msghttp.NewExchange: nil option at index 0")
				assert.NotContains(t, err.Error(), "NewConfig",
					"the caller never called NewConfig (ADR 0031 D-R)")
			},
		},
		{
			name: "NewExchange: nil element after a valid option",
			call: func(opts []msghttp.Option) error {
				_, err := msghttp.NewExchange("http://example.test/hook", opts...)
				return err
			},
			opts: []msghttp.Option{realOpt, nil},
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrNilFunc)
				assert.Contains(t, err.Error(), "msghttp.NewExchange: nil option at index 1")
			},
		},
		{
			name: "NewExchange: first of two nils wins",
			call: func(opts []msghttp.Option) error {
				_, err := msghttp.NewExchange("http://example.test/hook", opts...)
				return err
			},
			opts: []msghttp.Option{nil, nil},
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrNilFunc)
				assert.Contains(t, err.Error(), "msghttp.NewExchange: nil option at index 0")
			},
		},
		{
			// Precedence: the pre-check is the function's FIRST statement, so
			// it beats validateURL, which runs only after NewConfig returns.
			name: "NewExchange: the pre-check precedes the URL check",
			call: func(opts []msghttp.Option) error { _, err := msghttp.NewExchange("", opts...); return err },
			opts: []msghttp.Option{nil},
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrNilFunc)
				assert.NotErrorIs(t, err, msghttp.ErrInvalidURL)
				assert.Contains(t, err.Error(), "msghttp.NewExchange: nil option at index 0")
			},
		},

		// ---------------------------------------------------------------
		// msghttp.NewOutbound — DELEGATOR (same three-step shape as
		// NewExchange: NewConfig, then validateURL, then resolveClient).
		// ---------------------------------------------------------------
		{
			name: "NewOutbound: nil element alone names the delegator",
			call: func(opts []msghttp.Option) error {
				_, err := msghttp.NewOutbound("http://example.test/hook", opts...)
				return err
			},
			opts: []msghttp.Option{nil},
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrNilFunc)
				assert.False(t, msgin.IsPermanent(err),
					"R1 nil-option error must be BARE, not Permanent-wrapped")
				assert.Contains(t, err.Error(), "msghttp.NewOutbound: nil option at index 0")
				assert.NotContains(t, err.Error(), "NewConfig",
					"the caller never called NewConfig (ADR 0031 D-R)")
			},
		},
		{
			name: "NewOutbound: nil element after a valid option",
			call: func(opts []msghttp.Option) error {
				_, err := msghttp.NewOutbound("http://example.test/hook", opts...)
				return err
			},
			opts: []msghttp.Option{realOpt, nil},
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrNilFunc)
				assert.Contains(t, err.Error(), "msghttp.NewOutbound: nil option at index 1")
			},
		},
		{
			name: "NewOutbound: first of two nils wins",
			call: func(opts []msghttp.Option) error {
				_, err := msghttp.NewOutbound("http://example.test/hook", opts...)
				return err
			},
			opts: []msghttp.Option{nil, nil},
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrNilFunc)
				assert.Contains(t, err.Error(), "msghttp.NewOutbound: nil option at index 0")
			},
		},
		{
			name: "NewOutbound: the pre-check precedes the URL check",
			call: func(opts []msghttp.Option) error { _, err := msghttp.NewOutbound("", opts...); return err },
			opts: []msghttp.Option{nil},
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrNilFunc)
				assert.NotErrorIs(t, err, msghttp.ErrInvalidURL)
				assert.Contains(t, err.Error(), "msghttp.NewOutbound: nil option at index 0")
			},
		},

		// ---------------------------------------------------------------
		// msghttp.NewSSEServer — DELEGATOR. It takes NO argument other than
		// opts, so there is no argument fault for the pre-check to precede
		// and no precedence case to assert (unlike the other four).
		// ---------------------------------------------------------------
		{
			name: "NewSSEServer: nil element alone names the delegator",
			call: func(opts []msghttp.Option) error { _, err := msghttp.NewSSEServer(opts...); return err },
			opts: []msghttp.Option{nil},
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrNilFunc)
				assert.False(t, msgin.IsPermanent(err),
					"R1 nil-option error must be BARE, not Permanent-wrapped")
				assert.Contains(t, err.Error(), "msghttp.NewSSEServer: nil option at index 0")
				assert.NotContains(t, err.Error(), "NewConfig",
					"the caller never called NewConfig (ADR 0031 D-R)")
			},
		},
		{
			name: "NewSSEServer: nil element after a valid option",
			call: func(opts []msghttp.Option) error { _, err := msghttp.NewSSEServer(opts...); return err },
			opts: []msghttp.Option{realOpt, nil},
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrNilFunc)
				assert.Contains(t, err.Error(), "msghttp.NewSSEServer: nil option at index 1")
			},
		},
		{
			name: "NewSSEServer: first of two nils wins",
			call: func(opts []msghttp.Option) error { _, err := msghttp.NewSSEServer(opts...); return err },
			opts: []msghttp.Option{nil, nil},
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrNilFunc)
				assert.Contains(t, err.Error(), "msghttp.NewSSEServer: nil option at index 0")
			},
		},

		// ---------------------------------------------------------------
		// msghttp.NewSSEClient — DELEGATOR (NewConfig, then validateURL,
		// then resolveSSEClient).
		// ---------------------------------------------------------------
		{
			name: "NewSSEClient: nil element alone names the delegator",
			call: func(opts []msghttp.Option) error {
				_, err := msghttp.NewSSEClient("http://example.test/events", opts...)
				return err
			},
			opts: []msghttp.Option{nil},
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrNilFunc)
				assert.False(t, msgin.IsPermanent(err),
					"R1 nil-option error must be BARE, not Permanent-wrapped")
				assert.Contains(t, err.Error(), "msghttp.NewSSEClient: nil option at index 0")
				assert.NotContains(t, err.Error(), "NewConfig",
					"the caller never called NewConfig (ADR 0031 D-R)")
			},
		},
		{
			name: "NewSSEClient: nil element after a valid option",
			call: func(opts []msghttp.Option) error {
				_, err := msghttp.NewSSEClient("http://example.test/events", opts...)
				return err
			},
			opts: []msghttp.Option{realOpt, nil},
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrNilFunc)
				assert.Contains(t, err.Error(), "msghttp.NewSSEClient: nil option at index 1")
			},
		},
		{
			name: "NewSSEClient: first of two nils wins",
			call: func(opts []msghttp.Option) error {
				_, err := msghttp.NewSSEClient("http://example.test/events", opts...)
				return err
			},
			opts: []msghttp.Option{nil, nil},
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrNilFunc)
				assert.Contains(t, err.Error(), "msghttp.NewSSEClient: nil option at index 0")
			},
		},
		{
			name: "NewSSEClient: the pre-check precedes the URL check",
			call: func(opts []msghttp.Option) error { _, err := msghttp.NewSSEClient("", opts...); return err },
			opts: []msghttp.Option{nil},
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrNilFunc)
				assert.NotErrorIs(t, err, msghttp.ErrInvalidURL)
				assert.Contains(t, err.Error(), "msghttp.NewSSEClient: nil option at index 0")
			},
		},

		// ---------------------------------------------------------------
		// msghttp.NewSSEParser — DELEGATOR (NewConfig, then
		// newSSEParserWithCap, which is the first statement to touch r).
		// ---------------------------------------------------------------
		{
			name: "NewSSEParser: nil element alone names the delegator",
			call: func(opts []msghttp.Option) error {
				_, err := msghttp.NewSSEParser(strings.NewReader(""), opts...)
				return err
			},
			opts: []msghttp.Option{nil},
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrNilFunc)
				assert.False(t, msgin.IsPermanent(err),
					"R1 nil-option error must be BARE, not Permanent-wrapped")
				assert.Contains(t, err.Error(), "msghttp.NewSSEParser: nil option at index 0")
				assert.NotContains(t, err.Error(), "NewConfig",
					"the caller never called NewConfig (ADR 0031 D-R)")
			},
		},
		{
			name: "NewSSEParser: nil element after a valid option",
			call: func(opts []msghttp.Option) error {
				_, err := msghttp.NewSSEParser(strings.NewReader(""), opts...)
				return err
			},
			opts: []msghttp.Option{realOpt, nil},
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrNilFunc)
				assert.Contains(t, err.Error(), "msghttp.NewSSEParser: nil option at index 1")
			},
		},
		{
			name: "NewSSEParser: first of two nils wins",
			call: func(opts []msghttp.Option) error {
				_, err := msghttp.NewSSEParser(strings.NewReader(""), opts...)
				return err
			},
			opts: []msghttp.Option{nil, nil},
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrNilFunc)
				assert.Contains(t, err.Error(), "msghttp.NewSSEParser: nil option at index 0")
			},
		},
		{
			// NewSSEParser's godoc already promises the options are validated
			// "before r is touched"; the pre-check keeps that true — a nil r
			// would nil-deref inside newSSEParserWithCap's BOM Peek, which is
			// never reached.
			name: "NewSSEParser: the pre-check runs before r is touched",
			call: func(opts []msghttp.Option) error { _, err := msghttp.NewSSEParser(nil, opts...); return err },
			opts: []msghttp.Option{nil},
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrNilFunc)
				assert.Contains(t, err.Error(), "msghttp.NewSSEParser: nil option at index 0")
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

// TestNilOptionElement_SixDistinctPositions proves the SIX entry points really
// do produce six DIFFERENT position strings from the single msghttp.Option type
// — the property D-R exists to buy. A regression that dropped any delegator's
// pre-check would collapse that name onto "msghttp.NewConfig" and this test
// would fail on the duplicate, even if every individual position assertion above
// were relaxed to an errors.Is check.
func TestNilOptionElement_SixDistinctPositions(t *testing.T) {
	t.Parallel()

	calls := map[string]func() error{
		"msghttp.NewConfig": func() error { _, err := msghttp.NewConfig(nil); return err },
		"msghttp.NewExchange": func() error {
			_, err := msghttp.NewExchange("http://example.test/hook", nil)
			return err
		},
		"msghttp.NewOutbound": func() error {
			_, err := msghttp.NewOutbound("http://example.test/hook", nil)
			return err
		},
		"msghttp.NewSSEServer": func() error { _, err := msghttp.NewSSEServer(nil); return err },
		"msghttp.NewSSEClient": func() error {
			_, err := msghttp.NewSSEClient("http://example.test/events", nil)
			return err
		},
		"msghttp.NewSSEParser": func() error {
			_, err := msghttp.NewSSEParser(strings.NewReader(""), nil)
			return err
		},
	}

	seen := make(map[string]string, len(calls))
	for ctor, call := range calls {
		err := call()
		require.ErrorIs(t, err, msgin.ErrNilFunc, "%s", ctor)
		msg := err.Error()
		// assert, NOT require: a require here would abort the loop on the
		// first collapsed position and the duplicate branch below would never
		// run — a dead branch that proves nothing (vacuity probe: with any one
		// delegator's pre-check deleted, this test must report BOTH the wrong
		// position AND the duplicate).
		assert.Contains(t, msg, ctor+": nil option at index 0")
		if prev, dup := seen[msg]; dup {
			t.Errorf("%s and %s produce the SAME position %q — a delegator pre-check is missing", prev, ctor, msg)
		}
		seen[msg] = ctor
	}
	assert.Len(t, seen, len(calls), "each entry point must produce its own position string")
}
