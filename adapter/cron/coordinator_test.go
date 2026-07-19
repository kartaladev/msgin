package cron_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kartaladev/msgin"
	"github.com/kartaladev/msgin/adapter/cron"
)

// fakeElector / fakeLocker are hand-written doubles (no external dep). They record
// the call and return a scripted verdict/error.
type fakeElector struct {
	leader    bool
	err       error
	calls     int
	lastScope string
}

func (f *fakeElector) IsLeader(_ context.Context, scope string) (bool, error) {
	f.calls++
	f.lastScope = scope
	return f.leader, f.err
}

type fakeLocker struct {
	won       bool
	err       error
	lastScope string
	lastFire  time.Time
	calls     int
}

func (f *fakeLocker) Claim(_ context.Context, scope string, fire time.Time) (bool, error) {
	f.calls++
	f.lastScope, f.lastFire = scope, fire
	return f.won, f.err
}

// drainOne advances the fake clock to the first fire and reports whether a
// delivery arrived within a short window (true) or the fire was gated out (false).
func drainOne(t *testing.T, clk *clockwork.FakeClock, out <-chan msgin.Delivery, ctx context.Context) (msgin.Delivery, bool) {
	t.Helper()
	require.NoError(t, clk.BlockUntilContext(ctx, 1))
	clk.Advance(time.Hour)
	select {
	case d := <-out:
		return d, true
	case <-time.After(300 * time.Millisecond):
		return msgin.Delivery{}, false
	}
}

func TestSource_Gating(t *testing.T) {
	type testCase struct {
		name   string
		opts   func(fe *fakeElector, fl *fakeLocker) []cron.Option
		fe     fakeElector
		fl     fakeLocker
		assert func(t *testing.T, emitted bool, fe *fakeElector, fl *fakeLocker)
	}
	cases := []testCase{
		{
			name: "elector leader emits, receiving the Source's scope",
			fe:   fakeElector{leader: true},
			opts: func(fe *fakeElector, _ *fakeLocker) []cron.Option {
				return []cron.Option{cron.WithElector(fe), cron.WithScope("job-x")}
			},
			assert: func(t *testing.T, emitted bool, fe *fakeElector, _ *fakeLocker) {
				assert.True(t, emitted)
				assert.Equal(t, 1, fe.calls)
				assert.Equal(t, "job-x", fe.lastScope, "IsLeader must receive the Source's scope (M-1 symmetry with Locker.Claim)")
			},
		},
		{
			name:   "elector non-leader skips",
			fe:     fakeElector{leader: false},
			opts:   func(fe *fakeElector, _ *fakeLocker) []cron.Option { return []cron.Option{cron.WithElector(fe)} },
			assert: func(t *testing.T, emitted bool, _ *fakeElector, _ *fakeLocker) { assert.False(t, emitted) },
		},
		{
			name:   "elector error skips fail-safe",
			fe:     fakeElector{err: errors.New("db down")},
			opts:   func(fe *fakeElector, _ *fakeLocker) []cron.Option { return []cron.Option{cron.WithElector(fe)} },
			assert: func(t *testing.T, emitted bool, _ *fakeElector, _ *fakeLocker) { assert.False(t, emitted) },
		},
		{
			name: "locker winner emits and receives scope+fire",
			fl:   fakeLocker{won: true},
			opts: func(_ *fakeElector, fl *fakeLocker) []cron.Option {
				return []cron.Option{cron.WithLocker(fl), cron.WithScope("job-x")}
			},
			assert: func(t *testing.T, emitted bool, _ *fakeElector, fl *fakeLocker) {
				assert.True(t, emitted)
				assert.Equal(t, "job-x", fl.lastScope)
				assert.False(t, fl.lastFire.IsZero())
			},
		},
		{
			name:   "locker loser skips",
			fl:     fakeLocker{won: false},
			opts:   func(_ *fakeElector, fl *fakeLocker) []cron.Option { return []cron.Option{cron.WithLocker(fl)} },
			assert: func(t *testing.T, emitted bool, _ *fakeElector, _ *fakeLocker) { assert.False(t, emitted) },
		},
		{
			name:   "locker error skips fail-safe",
			fl:     fakeLocker{err: errors.New("db down")},
			opts:   func(_ *fakeElector, fl *fakeLocker) []cron.Option { return []cron.Option{cron.WithLocker(fl)} },
			assert: func(t *testing.T, emitted bool, _ *fakeElector, _ *fakeLocker) { assert.False(t, emitted) },
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fe, fl := tc.fe, tc.fl
			clk := clockwork.NewFakeClockAt(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
			opts := append(tc.opts(&fe, &fl), cron.WithClock(clk))
			// "@hourly" (grid-aligned) rather than "@every 1h": the Locker cases in
			// this table require a grid-aligned schedule (ErrLockerRequiresGridSchedule
			// refuses @every, tested separately below); the Elector cases work
			// identically with either. At this epoch (an exact hour boundary) both
			// specs fire at the same instant, so drainOne's Advance(time.Hour) behaves
			// identically.
			src, err := cron.NewSource("@hourly", func(time.Time) int { return 1 }, opts...)
			require.NoError(t, err)

			out := make(chan msgin.Delivery)
			ctx, cancel := context.WithCancel(t.Context())
			done := make(chan error, 1)
			go func() { done <- src.Stream(ctx, out) }()

			_, emitted := drainOne(t, clk, out, ctx)
			tc.assert(t, emitted, &fe, &fl)

			cancel()
			<-done
		})
	}
}

func TestSource_ConflictingCoordinator(t *testing.T) {
	_, err := cron.NewSource("@every 1h", func(time.Time) int { return 1 },
		cron.WithElector(&fakeElector{}), cron.WithLocker(&fakeLocker{}))
	assert.ErrorIs(t, err, cron.ErrConflictingCoordinator)
}

// TestSource_DefaultScopeIsSpec proves the WithScope default: with no WithScope,
// the Locker receives the raw spec string as the scope. Uses a grid-aligned
// spec ("@hourly") — the Locker refuses @every (see
// TestSource_LockerRequiresGridSchedule below).
func TestSource_DefaultScopeIsSpec(t *testing.T) {
	fl := fakeLocker{won: true}
	clk := clockwork.NewFakeClockAt(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	src, err := cron.NewSource("@hourly", func(time.Time) int { return 1 },
		cron.WithLocker(&fl), cron.WithClock(clk))
	require.NoError(t, err)

	out := make(chan msgin.Delivery)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- src.Stream(ctx, out) }()
	_, _ = drainOne(t, clk, out, ctx)
	cancel()
	<-done

	assert.Equal(t, "@hourly", fl.lastScope)
}

// TestSource_LockerRequiresGridSchedule proves the Round-1 audit B-1 fix: a
// Locker paired with an "@every" schedule is refused at construction, because
// robfig's ConstantDelaySchedule.Next(t) = t+Delay is relative to each
// instance's own last-fire/start time — independent instances never converge
// on the same dedup key, so the Locker would silently dedup nothing. Cron and
// descriptor specs (grid-aligned) are unaffected — construct fine with a Locker.
func TestSource_LockerRequiresGridSchedule(t *testing.T) {
	type testCase struct {
		name   string
		spec   string
		assert func(t *testing.T, err error)
	}
	cases := []testCase{
		{
			name: "@every + Locker is refused",
			spec: "@every 1h",
			assert: func(t *testing.T, err error) {
				assert.ErrorIs(t, err, cron.ErrLockerRequiresGridSchedule)
			},
		},
		{
			name:   "5-field cron + Locker constructs fine",
			spec:   "0 * * * *",
			assert: func(t *testing.T, err error) { assert.NoError(t, err) },
		},
		{
			name:   "@hourly descriptor + Locker constructs fine",
			spec:   "@hourly",
			assert: func(t *testing.T, err error) { assert.NoError(t, err) },
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := cron.NewSource(tc.spec, func(time.Time) int { return 1 }, cron.WithLocker(&fakeLocker{}))
			tc.assert(t, err)
		})
	}
}
