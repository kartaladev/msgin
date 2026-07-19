package cron_test

import (
	"testing"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kartaladev/msgin/adapter/cron"
)

// TestSource_WithSeconds proves a 6-field (seconds) schedule fires at the
// sub-minute instant when WithSeconds is set. Reuses startStream + the
// fake-clock BlockUntilContext->Advance pattern from source_test.go.
func TestSource_WithSeconds(t *testing.T) {
	epoch := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	type testCase struct {
		name   string
		spec   string
		step   time.Duration
		assert func(t *testing.T, fire time.Time)
	}
	cases := []testCase{
		{
			name: "every 5 seconds fires at +5s",
			spec: "*/5 * * * * *", step: 5 * time.Second,
			assert: func(t *testing.T, fire time.Time) {
				assert.Equal(t, epoch.Add(5*time.Second), fire.UTC())
			},
		},
		{
			name: "second-0 of each minute fires at the next minute boundary",
			spec: "0 * * * * *", step: time.Minute,
			assert: func(t *testing.T, fire time.Time) {
				assert.Equal(t, epoch.Add(time.Minute), fire.UTC())
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clk := clockwork.NewFakeClockAt(epoch)
			src, err := cron.NewSource(tc.spec, func(fire time.Time) time.Time { return fire },
				cron.WithClock(clk), cron.WithSeconds())
			require.NoError(t, err)

			out, ctx, stop := startStream(t, src)
			defer stop()

			require.NoError(t, clk.BlockUntilContext(ctx, 1))
			clk.Advance(tc.step)

			select {
			case d := <-out:
				tc.assert(t, d.Msg.Payload().(time.Time))
			case <-time.After(2 * time.Second):
				t.Fatal("expected a fire, got none")
			}
		})
	}
}

// TestSource_WithSecondsValidation locks the required-6-field contract and the
// unchanged default: with WithSeconds a 5-field spec is ErrInvalidSchedule;
// without it a 6-field spec is ErrInvalidSchedule; @every/descriptors still work.
func TestSource_WithSecondsValidation(t *testing.T) {
	type testCase struct {
		name   string
		build  func() (any, error)
		assert func(t *testing.T, err error)
	}
	cases := []testCase{
		{
			name: "WithSeconds + valid 6-field constructs",
			build: func() (any, error) {
				return cron.NewSource("*/5 * * * * *", func(time.Time) int { return 0 }, cron.WithSeconds())
			},
			assert: func(t *testing.T, err error) { assert.NoError(t, err) },
		},
		{
			name: "WithSeconds + 5-field spec is ErrInvalidSchedule",
			build: func() (any, error) {
				return cron.NewSource("* * * * *", func(time.Time) int { return 0 }, cron.WithSeconds())
			},
			assert: func(t *testing.T, err error) { assert.ErrorIs(t, err, cron.ErrInvalidSchedule) },
		},
		{
			name:   "default (no WithSeconds) + 6-field spec is ErrInvalidSchedule",
			build:  func() (any, error) { return cron.NewSource("*/5 * * * * *", func(time.Time) int { return 0 }) },
			assert: func(t *testing.T, err error) { assert.ErrorIs(t, err, cron.ErrInvalidSchedule) },
		},
		{
			name: "WithSeconds + @hourly descriptor constructs",
			build: func() (any, error) {
				return cron.NewSource("@hourly", func(time.Time) int { return 0 }, cron.WithSeconds())
			},
			assert: func(t *testing.T, err error) { assert.NoError(t, err) },
		},
		{
			name: "WithSeconds + @every constructs",
			build: func() (any, error) {
				return cron.NewSource("@every 30s", func(time.Time) int { return 0 }, cron.WithSeconds())
			},
			assert: func(t *testing.T, err error) { assert.NoError(t, err) },
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.build()
			tc.assert(t, err)
		})
	}
}

// TestSource_WithSecondsLockerGridAlignment proves a 6-field cron is still
// grid-aligned, so a Locker is accepted (unlike @every, which stays refused).
func TestSource_WithSecondsLockerGridAlignment(t *testing.T) {
	type testCase struct {
		name   string
		spec   string
		assert func(t *testing.T, err error)
	}
	cases := []testCase{
		{
			name:   "6-field cron + Locker constructs (grid-aligned)",
			spec:   "*/5 * * * * *",
			assert: func(t *testing.T, err error) { assert.NoError(t, err) },
		},
		{
			name:   "@every + Locker still refused under WithSeconds",
			spec:   "@every 30s",
			assert: func(t *testing.T, err error) { assert.ErrorIs(t, err, cron.ErrLockerRequiresGridSchedule) },
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Reuse the existing fakeLocker double from coordinator_test.go (same
			// package cron_test) — it has a POINTER receiver, so pass &fakeLocker{}.
			_, err := cron.NewSource(tc.spec, func(time.Time) int { return 0 },
				cron.WithSeconds(), cron.WithLocker(&fakeLocker{won: true}))
			tc.assert(t, err)
		})
	}
}
