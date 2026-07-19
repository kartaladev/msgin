package msgin_test

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/kartaladev/msgin"
	"github.com/kartaladev/msgin/adapter/memory"
)

// scheduleLogSink is a tiny OutboundAdapter that also implements
// msgin.ScheduledSender, printing the delay it is asked to schedule.
type scheduleLogSink struct{}

func (scheduleLogSink) Send(context.Context, msgin.Message[any]) error {
	fmt.Println("send now")
	return nil
}

func (scheduleLogSink) SendAfter(_ context.Context, _ msgin.Message[any], d time.Duration) error {
	fmt.Printf("send after %s\n", d)
	return nil
}

// ExampleProducer_SendAfter shows a durable delayed send against a
// scheduling-capable adapter, and the fail-loud error when the adapter cannot
// schedule (the in-memory Broker does not implement ScheduledSender).
func ExampleProducer_SendAfter() {
	// A scheduling-capable sink (the real durable one is the database/sql adapter).
	sched, _ := msgin.NewProducer[string](scheduleLogSink{})
	_ = sched.SendAfter(context.Background(), msgin.New("payload"), 2*time.Hour)

	// A non-scheduling sink fails loud rather than delivering immediately.
	mem, _ := msgin.NewProducer[string](memory.New())
	err := mem.SendAfter(context.Background(), msgin.New("payload"), time.Minute)
	fmt.Println(errors.Is(err, msgin.ErrScheduledSendUnsupported))
	// Output:
	// send after 2h0m0s
	// true
}
