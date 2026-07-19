package cron_test

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kartaladev/msgin"
	"github.com/kartaladev/msgin/adapter/cron"
)

// recordingHandler is a minimal slog.Handler that captures every record it
// receives, so tests can assert on log LEVEL without parsing text/JSON
// output. Safe for concurrent use (Stream's firing loop runs on its own
// goroutine).
type recordingHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *recordingHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *recordingHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r)
	return nil
}

func (h *recordingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *recordingHandler) WithGroup(string) slog.Handler      { return h }

// hasLevel reports whether any captured record was emitted at exactly level.
func (h *recordingHandler) hasLevel(level slog.Level) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, r := range h.records {
		if r.Level == level {
			return true
		}
	}
	return false
}

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

// TestSource_WinLogsCoordinatorErrorsExceptCancellation locks both branches of
// win()'s logging guard: a context.Canceled/DeadlineExceeded gate error is a
// graceful-shutdown signal, not a genuine coordinator failure, so it must NOT
// be logged at ERROR (that would trip alerts on every graceful shutdown of a
// coordinated Source); any OTHER coordinator error is genuine and must still
// be logged at ERROR. Both cases still fail-safe skip the fire either way
// (proven separately by TestSource_Gating's "elector/locker error skips
// fail-safe" cases).
func TestSource_WinLogsCoordinatorErrorsExceptCancellation(t *testing.T) {
	type testCase struct {
		name       string
		gateErr    error
		assertLogs func(t *testing.T, h *recordingHandler)
	}
	cases := []testCase{
		{
			name:    "context.Canceled is skipped without an ERROR log (graceful shutdown, not a genuine failure)",
			gateErr: context.Canceled,
			assertLogs: func(t *testing.T, h *recordingHandler) {
				assert.False(t, h.hasLevel(slog.LevelError), "context.Canceled must not be logged at ERROR")
			},
		},
		{
			name:    "context.DeadlineExceeded is skipped without an ERROR log (same shutdown-signal treatment)",
			gateErr: context.DeadlineExceeded,
			assertLogs: func(t *testing.T, h *recordingHandler) {
				assert.False(t, h.hasLevel(slog.LevelError), "context.DeadlineExceeded must not be logged at ERROR")
			},
		},
		{
			name:    "a genuine coordinator error IS logged at ERROR (locks the other branch)",
			gateErr: errors.New("db down"),
			assertLogs: func(t *testing.T, h *recordingHandler) {
				assert.True(t, h.hasLevel(slog.LevelError), "a genuine coordinator error must still be logged at ERROR")
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := &recordingHandler{}
			fe := &fakeElector{err: tc.gateErr}
			clk := clockwork.NewFakeClockAt(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
			src, err := cron.NewSource("@hourly", func(time.Time) int { return 1 },
				cron.WithElector(fe), cron.WithClock(clk), cron.WithCronLogger(slog.New(h)))
			require.NoError(t, err)

			out := make(chan msgin.Delivery)
			ctx, cancel := context.WithCancel(t.Context())
			done := make(chan error, 1)
			go func() { done <- src.Stream(ctx, out) }()

			_, emitted := drainOne(t, clk, out, ctx)
			assert.False(t, emitted, "a coordinator error must fail-safe skip the fire")

			cancel()
			<-done

			tc.assertLogs(t, h)
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
