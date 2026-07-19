package cron_test

import (
	"context"
	"fmt"
	"time"

	"github.com/jonboulle/clockwork"

	"github.com/kartaladev/msgin"
	"github.com/kartaladev/msgin/adapter/cron"
)

// ExampleSource shows a recurring source: an "@every 1h" schedule emits a
// message carrying the fire time. A fake clock makes the output deterministic;
// in production you would pass the source to msgin.NewConsumer and Run it.
func ExampleSource() {
	epoch := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clk := clockwork.NewFakeClockAt(epoch)

	src, err := cron.NewSource("@every 1h",
		func(fire time.Time) string { return "tick at " + fire.UTC().Format("15:04") },
		cron.WithClock(clk))
	if err != nil {
		panic(err)
	}

	out := make(chan msgin.Delivery)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- src.Stream(ctx, out) }()

	_ = clk.BlockUntilContext(ctx, 1)
	clk.Advance(time.Hour)
	d := <-out
	fmt.Println(d.Msg.Payload())

	// Join the Stream goroutine before returning so the package's goleak
	// check (TestMain) never observes it mid-exit.
	cancel()
	<-done

	// Output:
	// tick at 01:00
}
