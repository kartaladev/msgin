package msghttp_test

import (
	"testing"

	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	msghttp "github.com/kartaladev/msgin/adapter/http"
)

// TestNewConfig_sseServer exercises the SSE server option surface added in
// Plan 025 Task 3 (NewSSEServer itself does not exist yet — that is Tasks
// 4-5). Config's SSE-server fields are unexported with no getters, so a
// blackbox (package msghttp_test) test can observe only NewConfig's returned
// error, never a resolved value — mirroring TestNewConfig_outbound's
// (adapter/http/outbound_test.go) precedent for the same constraint.
//
// BEHAVIORAL-DEFERRAL NOTE (the Plan 024 Task-1 rule, restated here per the
// Task 3 brief): the *defaults* (1024 connections / 16-event buffer / drop
// policy / replay off / heartbeat off / 30s write timeout) are
// line-covered by this table's unset-options cases ("construction succeeds
// + the resolved value is not re-validated"), NOT behaviorally proven —
// proving the 1024-connection default behaviorally would require standing
// up 1024 live connections, which is impractical here. Where a default's
// *behavior* is observable at all, it is proven with a small explicit
// override instead (e.g. WithMaxConnections(2) in Task 4). This file does
// not fabricate a behavioral test for a server that doesn't exist yet.
func TestNewConfig_sseServer(t *testing.T) {
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
			name:   "sse server defaults are valid",
			assert: valid,
		},
		{
			name: "WithMaxConnections(0) is invalid",
			opts: []msghttp.Option{msghttp.WithMaxConnections(0)},
			assert: func(t *testing.T, cfg *msghttp.Config, err error) {
				assert.Nil(t, cfg)
				assert.ErrorIs(t, err, msghttp.ErrInvalidMaxConnections)
			},
		},
		{
			name: "WithMaxConnections(-1) is invalid",
			opts: []msghttp.Option{msghttp.WithMaxConnections(-1)},
			assert: func(t *testing.T, cfg *msghttp.Config, err error) {
				assert.Nil(t, cfg)
				assert.ErrorIs(t, err, msghttp.ErrInvalidMaxConnections)
			},
		},
		{
			name:   "WithMaxConnections(1) is accepted",
			opts:   []msghttp.Option{msghttp.WithMaxConnections(1)},
			assert: valid,
		},
		{
			name: "WithConnectionBuffer(0) is invalid",
			opts: []msghttp.Option{msghttp.WithConnectionBuffer(0)},
			assert: func(t *testing.T, cfg *msghttp.Config, err error) {
				assert.Nil(t, cfg)
				assert.ErrorIs(t, err, msghttp.ErrInvalidConnectionBuffer)
			},
		},
		{
			name: "WithConnectionBuffer(-1) is invalid",
			opts: []msghttp.Option{msghttp.WithConnectionBuffer(-1)},
			assert: func(t *testing.T, cfg *msghttp.Config, err error) {
				assert.Nil(t, cfg)
				assert.ErrorIs(t, err, msghttp.ErrInvalidConnectionBuffer)
			},
		},
		{
			name:   "WithConnectionBuffer(1) is accepted",
			opts:   []msghttp.Option{msghttp.WithConnectionBuffer(1)},
			assert: valid,
		},
		{
			name: "WithSlowClientPolicy(99) is invalid",
			opts: []msghttp.Option{msghttp.WithSlowClientPolicy(msghttp.SlowClientPolicy(99))},
			assert: func(t *testing.T, cfg *msghttp.Config, err error) {
				assert.Nil(t, cfg)
				assert.ErrorIs(t, err, msghttp.ErrInvalidSlowClientPolicy)
			},
		},
		{
			name: "WithSlowClientPolicy(-1) is invalid",
			opts: []msghttp.Option{msghttp.WithSlowClientPolicy(msghttp.SlowClientPolicy(-1))},
			assert: func(t *testing.T, cfg *msghttp.Config, err error) {
				assert.Nil(t, cfg)
				assert.ErrorIs(t, err, msghttp.ErrInvalidSlowClientPolicy)
			},
		},
		{
			name:   "WithSlowClientPolicy(SlowClientDrop) is accepted",
			opts:   []msghttp.Option{msghttp.WithSlowClientPolicy(msghttp.SlowClientDrop)},
			assert: valid,
		},
		{
			name:   "WithSlowClientPolicy(SlowClientDisconnect) is accepted",
			opts:   []msghttp.Option{msghttp.WithSlowClientPolicy(msghttp.SlowClientDisconnect)},
			assert: valid,
		},
		{
			name: "WithReplayBuffer(0) is invalid",
			opts: []msghttp.Option{msghttp.WithReplayBuffer(0)},
			assert: func(t *testing.T, cfg *msghttp.Config, err error) {
				assert.Nil(t, cfg)
				assert.ErrorIs(t, err, msghttp.ErrInvalidReplayBuffer)
			},
		},
		{
			name: "WithReplayBuffer(-1) is invalid",
			opts: []msghttp.Option{msghttp.WithReplayBuffer(-1)},
			assert: func(t *testing.T, cfg *msghttp.Config, err error) {
				assert.Nil(t, cfg)
				assert.ErrorIs(t, err, msghttp.ErrInvalidReplayBuffer)
			},
		},
		{
			name:   "WithReplayBuffer(1) is accepted",
			opts:   []msghttp.Option{msghttp.WithReplayBuffer(1)},
			assert: valid,
		},
		{
			name: "WithHeartbeat(0) is invalid",
			opts: []msghttp.Option{msghttp.WithHeartbeat(0)},
			assert: func(t *testing.T, cfg *msghttp.Config, err error) {
				assert.Nil(t, cfg)
				assert.ErrorIs(t, err, msghttp.ErrInvalidHeartbeat)
			},
		},
		{
			name: "WithHeartbeat(-1) is invalid",
			opts: []msghttp.Option{msghttp.WithHeartbeat(-1)},
			assert: func(t *testing.T, cfg *msghttp.Config, err error) {
				assert.Nil(t, cfg)
				assert.ErrorIs(t, err, msghttp.ErrInvalidHeartbeat)
			},
		},
		{
			name:   "WithHeartbeat(1) is accepted",
			opts:   []msghttp.Option{msghttp.WithHeartbeat(1)},
			assert: valid,
		},
		{
			name: "WithWriteTimeout(0) is invalid",
			opts: []msghttp.Option{msghttp.WithWriteTimeout(0)},
			assert: func(t *testing.T, cfg *msghttp.Config, err error) {
				assert.Nil(t, cfg)
				assert.ErrorIs(t, err, msghttp.ErrInvalidWriteTimeout)
			},
		},
		{
			name: "WithWriteTimeout(-1) is invalid",
			opts: []msghttp.Option{msghttp.WithWriteTimeout(-1)},
			assert: func(t *testing.T, cfg *msghttp.Config, err error) {
				assert.Nil(t, cfg)
				assert.ErrorIs(t, err, msghttp.ErrInvalidWriteTimeout)
			},
		},
		{
			name:   "WithWriteTimeout(1) is accepted",
			opts:   []msghttp.Option{msghttp.WithWriteTimeout(1)},
			assert: valid,
		},
		{
			name:   "WithSSEClock(nil) is a no-op",
			opts:   []msghttp.Option{msghttp.WithSSEClock(nil)},
			assert: valid,
		},
		{
			name:   "WithSSEClock with a fake clock is accepted",
			opts:   []msghttp.Option{msghttp.WithSSEClock(clockwork.NewFakeClock())},
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
