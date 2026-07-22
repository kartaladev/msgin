package msghttp_test

import (
	"net/http"
	"testing"

	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	msghttp "github.com/kartaladev/msgin/adapter/http"
)

// TestNewConfig_outbound exercises the outbound option surface added in Plan
// 024 Task 1. Config's outbound fields are unexported with no getters, so a
// blackbox (package msghttp_test) test can observe only NewConfig's returned
// error, never a resolved value. Only WithMaxResponseBytes(<=0) is therefore a
// genuine BEHAVIORAL assertion here (round-2 audit F3): it asserts through the
// ErrInvalidMaxResponseBytes error contract and can fail red-first. The
// remaining rows are line-covered only — they assert construction
// succeeds/fails as the error contract dictates, and are NOT dressed up as
// value checks. Their behavioral correctness (the 1 MiB default, the kept
// default client/clock, the cloned allow-lists) becomes observable, and is
// asserted, in Tasks 2/4/5 where the value is reachable.
func TestNewConfig_outbound(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name   string
		opts   []msghttp.Option
		assert func(t *testing.T, cfg *msghttp.Config, err error)
	}

	valid := func(t *testing.T, cfg *msghttp.Config, err error) {
		t.Helper()
		require.NoError(t, err)
		require.NotNil(t, cfg)
	}

	cases := []testCase{
		{
			name:   "outbound defaults are valid",
			assert: valid,
		},
		{
			name: "WithMaxResponseBytes(0) is invalid",
			opts: []msghttp.Option{msghttp.WithMaxResponseBytes(0)},
			assert: func(t *testing.T, cfg *msghttp.Config, err error) {
				assert.Nil(t, cfg)
				assert.ErrorIs(t, err, msghttp.ErrInvalidMaxResponseBytes)
			},
		},
		{
			name: "WithMaxResponseBytes(-1) is invalid",
			opts: []msghttp.Option{msghttp.WithMaxResponseBytes(-1)},
			assert: func(t *testing.T, cfg *msghttp.Config, err error) {
				assert.Nil(t, cfg)
				assert.ErrorIs(t, err, msghttp.ErrInvalidMaxResponseBytes)
			},
		},
		{
			name:   "WithMaxResponseBytes(1) is accepted",
			opts:   []msghttp.Option{msghttp.WithMaxResponseBytes(1)},
			assert: valid,
		},
		{
			name:   "WithHTTPClient(nil) is a no-op",
			opts:   []msghttp.Option{msghttp.WithHTTPClient(nil)},
			assert: valid,
		},
		{
			name:   "WithHTTPClient with a custom client is accepted",
			opts:   []msghttp.Option{msghttp.WithHTTPClient(&http.Client{})},
			assert: valid,
		},
		{
			name:   "WithOutboundClock(nil) is a no-op",
			opts:   []msghttp.Option{msghttp.WithOutboundClock(nil)},
			assert: valid,
		},
		{
			name:   "WithOutboundClock with a fake clock is accepted",
			opts:   []msghttp.Option{msghttp.WithOutboundClock(clockwork.NewFakeClock())},
			assert: valid,
		},
		{
			name:   "WithFollowRedirects is accepted",
			opts:   []msghttp.Option{msghttp.WithFollowRedirects()},
			assert: valid,
		},
		{
			name:   "WithOutboundHeaders is accepted",
			opts:   []msghttp.Option{msghttp.WithOutboundHeaders("X-A", "X-B")},
			assert: valid,
		},
		{
			name:   "WithOutboundReplyHeaders is accepted",
			opts:   []msghttp.Option{msghttp.WithOutboundReplyHeaders("X-A", "X-B")},
			assert: valid,
		},
		{
			name:   "WithErrorBodyExcerpt is accepted",
			opts:   []msghttp.Option{msghttp.WithErrorBodyExcerpt()},
			assert: valid,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg, err := msghttp.NewConfig(tc.opts...)
			tc.assert(t, cfg, err)
		})
	}
}
