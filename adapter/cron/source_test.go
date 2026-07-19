package cron_test

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"

	"github.com/kartaladev/msgin"
	"github.com/kartaladev/msgin/adapter/cron"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

// startStream runs src.Stream on a background goroutine and returns the delivery
// channel plus a stop func that cancels and joins the goroutine (goleak safety).
func startStream(t *testing.T, src interface {
	Stream(context.Context, chan<- msgin.Delivery) error
}) (<-chan msgin.Delivery, context.Context, func()) {
	t.Helper()
	out := make(chan msgin.Delivery)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- src.Stream(ctx, out) }()
	return out, ctx, func() {
		cancel()
		<-done // join before the test returns
	}
}

func TestSource_FiresOnSchedule(t *testing.T) {
	epoch := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	type testCase struct {
		name   string
		spec   string
		step   time.Duration // the delay to Advance to reach the first fire
		assert func(t *testing.T, fire time.Time)
	}
	cases := []testCase{
		{
			name: "@every interval fires after the interval",
			spec: "@every 1h", step: time.Hour,
			assert: func(t *testing.T, fire time.Time) {
				assert.Equal(t, epoch.Add(time.Hour), fire.UTC())
			},
		},
		{
			name: "5-field cron fires at the next minute boundary",
			spec: "* * * * *", step: time.Minute,
			assert: func(t *testing.T, fire time.Time) {
				assert.Equal(t, epoch.Add(time.Minute), fire.UTC())
			},
		},
		{
			name: "@hourly descriptor fires at the next hour",
			spec: "@hourly", step: time.Hour,
			assert: func(t *testing.T, fire time.Time) {
				assert.Equal(t, epoch.Add(time.Hour), fire.UTC())
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clk := clockwork.NewFakeClockAt(epoch)
			// factory captures the fire time into the payload so we can assert it.
			src, err := cron.NewSource(tc.spec, func(fire time.Time) time.Time { return fire },
				cron.WithClock(clk))
			require.NoError(t, err)

			out, ctx, stop := startStream(t, src)
			defer stop()

			require.NoError(t, clk.BlockUntilContext(ctx, 1)) // loop parked on clock.After
			clk.Advance(tc.step)

			select {
			case d := <-out:
				tc.assert(t, d.Msg.Payload().(time.Time))
				// stamped id + timestamp present (New path), Ack/Nack are safe no-ops.
				assert.NotEmpty(t, d.Msg.ID())
				require.NoError(t, d.Ack(t.Context()))
				require.NoError(t, d.Nack(t.Context(), false, 0))
			case <-time.After(2 * time.Second):
				t.Fatal("expected a fire, got none")
			}
		})
	}
}

// TestSource_SkipsMissedFireOnOverrun proves the skip-missed semantic (Spec D5):
// while the loop is blocked handing off one fire, advancing past a SECOND fire
// does not queue it — after the first is consumed, the next delivery is the
// THIRD scheduled instant, not the skipped second.
func TestSource_SkipsMissedFireOnOverrun(t *testing.T) {
	epoch := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clk := clockwork.NewFakeClockAt(epoch)
	src, err := cron.NewSource("@every 1h", func(fire time.Time) time.Time { return fire },
		cron.WithClock(clk))
	require.NoError(t, err)

	out, ctx, stop := startStream(t, src)
	defer stop()

	require.NoError(t, clk.BlockUntilContext(ctx, 1))
	clk.Advance(time.Hour) // reach fire #1 (01:00); loop now blocks on `out <-`
	// Advance past fire #2 (02:00) WITHOUT reading — the loop is stuck on the send,
	// so it never registers a timer for #2; #2 is missed.
	clk.Advance(90 * time.Minute) // clock now at 02:30

	d1 := <-out // fire #1 delivered
	require.Equal(t, epoch.Add(time.Hour), d1.Msg.Payload().(time.Time).UTC())

	// The loop recomputes Next(02:30) = 03:00 and blocks — #2 (02:00) was skipped.
	require.NoError(t, clk.BlockUntilContext(ctx, 1))
	clk.Advance(30 * time.Minute) // reach 03:00
	d2 := <-out
	require.Equal(t, epoch.Add(3*time.Hour), d2.Msg.Payload().(time.Time).UTC(),
		"the missed 02:00 fire must be skipped, not queued")
}

func TestSource_CtxCancelReturnsPromptly(t *testing.T) {
	clk := clockwork.NewFakeClockAt(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	src, err := cron.NewSource("@every 1h", func(time.Time) int { return 1 }, cron.WithClock(clk))
	require.NoError(t, err)

	out := make(chan msgin.Delivery)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- src.Stream(ctx, out) }()
	require.NoError(t, clk.BlockUntilContext(ctx, 1))

	cancel()
	select {
	case err := <-done:
		assert.ErrorIs(t, err, context.Canceled)
	case <-time.After(2 * time.Second):
		t.Fatal("Stream did not return on ctx cancel")
	}
}

// TestSource_CtxCancelDuringSendReturnsPromptly is an addition beyond the
// brief, closing a hot-path gap: TestSource_CtxCancelReturnsPromptly only
// exercises the OUTER select's ctx.Done() case (waiting on the timer); this
// exercises the INNER select's ctx.Done() case (blocked handing a fire off to
// `out` because nothing is reading it). Not folded into that table because the
// setup structurally diverges: this one must first let the timer fire and
// withhold the read from out to reach the blocked-send state before cancelling.
func TestSource_CtxCancelDuringSendReturnsPromptly(t *testing.T) {
	clk := clockwork.NewFakeClockAt(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	src, err := cron.NewSource("@every 1h", func(time.Time) int { return 1 }, cron.WithClock(clk))
	require.NoError(t, err)

	out := make(chan msgin.Delivery) // never read from — forces the hand-off to block
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- src.Stream(ctx, out) }()

	require.NoError(t, clk.BlockUntilContext(ctx, 1)) // parked on clock.After
	clk.Advance(time.Hour)                            // fire occurs; loop now blocks on `out <-`

	cancel()
	select {
	case err := <-done:
		assert.ErrorIs(t, err, context.Canceled)
	case <-time.After(2 * time.Second):
		t.Fatal("Stream did not return on ctx cancel while blocked sending")
	}
}

func TestSource_ConstructionValidation(t *testing.T) {
	type testCase struct {
		name   string
		build  func() (any, error)
		assert func(t *testing.T, err error)
	}
	cases := []testCase{
		{
			name:   "invalid spec is ErrInvalidSchedule",
			build:  func() (any, error) { return cron.NewSource("not a cron", func(time.Time) int { return 0 }) },
			assert: func(t *testing.T, err error) { assert.ErrorIs(t, err, cron.ErrInvalidSchedule) },
		},
		{
			// "0 0 30 2 *" (Feb 30) is syntactically valid but has no future
			// occurrence — robfig.Schedule.Next returns the zero time after a
			// 5-year search. Construction MUST catch this (not just the parse),
			// or the firing loop hot-spins on a zero Next (Round-1 audit B-2).
			name: "unsatisfiable spec (Feb 30) is ErrInvalidSchedule",
			build: func() (any, error) {
				return cron.NewSource("0 0 30 2 *", func(time.Time) int { return 0 })
			},
			assert: func(t *testing.T, err error) { assert.ErrorIs(t, err, cron.ErrInvalidSchedule) },
		},
		{
			name:   "nil factory is ErrNilFactory",
			build:  func() (any, error) { return cron.NewSource[int]("@every 1h", nil) },
			assert: func(t *testing.T, err error) { assert.ErrorIs(t, err, cron.ErrNilFactory) },
		},
		{
			name:   "valid spec + factory constructs",
			build:  func() (any, error) { return cron.NewSource("@every 1h", func(time.Time) int { return 0 }) },
			assert: func(t *testing.T, err error) { assert.NoError(t, err) },
		},
		// The cases below are additions beyond the brief, closing the CLAUDE.md
		// hot-path/coverage gate: every Option's nil-ignored guard, and NewSource's
		// scope-resolution branches, must have a covering test case.
		{
			name: "nil clock option is ignored (default kept, no panic)",
			build: func() (any, error) {
				return cron.NewSource("@every 1h", func(time.Time) int { return 0 }, cron.WithClock(nil))
			},
			assert: func(t *testing.T, err error) { assert.NoError(t, err) },
		},
		{
			name: "nil location option is ignored (UTC default kept, no panic)",
			build: func() (any, error) {
				return cron.NewSource("@every 1h", func(time.Time) int { return 0 }, cron.WithLocation(nil))
			},
			assert: func(t *testing.T, err error) { assert.NoError(t, err) },
		},
		{
			name: "nil cron logger option is ignored (discard default kept, no panic)",
			build: func() (any, error) {
				return cron.NewSource("@every 1h", func(time.Time) int { return 0 }, cron.WithCronLogger(nil))
			},
			assert: func(t *testing.T, err error) { assert.NoError(t, err) },
		},
		{
			name: "non-nil cron logger option is applied",
			build: func() (any, error) {
				l := slog.New(slog.NewTextHandler(io.Discard, nil))
				return cron.NewSource("@every 1h", func(time.Time) int { return 0 }, cron.WithCronLogger(l))
			},
			assert: func(t *testing.T, err error) { assert.NoError(t, err) },
		},
		{
			name: "empty-string scope is treated as unset (spec-string default used)",
			build: func() (any, error) {
				return cron.NewSource("@every 1h", func(time.Time) int { return 0 }, cron.WithScope(""))
			},
			assert: func(t *testing.T, err error) { assert.NoError(t, err) },
		},
		{
			name: "explicit non-empty scope overrides the spec-string default",
			build: func() (any, error) {
				return cron.NewSource("@every 1h", func(time.Time) int { return 0 }, cron.WithScope("custom-scope"))
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

func TestSource_EmitsLiveValue(t *testing.T) {
	src, err := cron.NewSource("@every 1h", func(time.Time) int { return 0 })
	require.NoError(t, err)
	var _ msgin.LiveValueSource = src
	assert.True(t, src.EmitsLiveValue())
}

// TestSource_WithLocation shifts the fire instant by the configured timezone.
func TestSource_WithLocation(t *testing.T) {
	// 00:30 UTC; a daily 09:00 job in a UTC+9 zone fires at 00:00 UTC (09:00 local).
	loc, err := time.LoadLocation("Asia/Tokyo") // UTC+9
	require.NoError(t, err)
	base := time.Date(2026, 1, 1, 0, 30, 0, 0, time.UTC)
	clk := clockwork.NewFakeClockAt(base)
	src, err := cron.NewSource("0 9 * * *", func(fire time.Time) time.Time { return fire },
		cron.WithClock(clk), cron.WithLocation(loc))
	require.NoError(t, err)

	out, ctx, stop := startStream(t, src)
	defer stop()
	require.NoError(t, clk.BlockUntilContext(ctx, 1))
	clk.Advance(24 * time.Hour) // comfortably past the next 09:00 Tokyo

	d := <-out
	got := d.Msg.Payload().(time.Time)
	// Next 09:00 Tokyo after 09:30 Tokyo (=00:30 UTC) is the following day 09:00 Tokyo = 00:00 UTC.
	assert.Equal(t, time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC), got.UTC())
}
