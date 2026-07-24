package msghttp_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"time"

	"github.com/kartaladev/msgin"
	msghttp "github.com/kartaladev/msgin/adapter/http"
)

// ExampleNewSSEClient streams two Server-Sent Events from a tiny
// httptest.Server and prints each as it arrives.
//
// Determinism note: the FIRST connection writes both events and then returns
// from the handler, ending that response with a clean EOF (no hold-open, no
// goroutine racing the test); Stream reconnects once — with
// WithReconnectBackoff shrunk to a trivial delay so the example does not wait
// on the library's real 500ms default — and the SECOND (and every further)
// connection answers 204, the server's deliberate stop, which ends Stream
// with a nil error on its own. Nothing here depends on a race between a
// manual cancel and a timer: the whole run is driven by the fixed
// two-connections script, so no time.Sleep or retry loop is needed.
func ExampleNewSSEClient() {
	var hits atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits.Add(1) == 1 {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("id: 1\nevent: greeting\ndata: hello\n\n"))
			_, _ = w.Write([]byte("id: 2\nevent: greeting\ndata: world\n\n"))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer ts.Close()

	client, err := msghttp.NewSSEClient(ts.URL, msghttp.WithReconnectBackoff(time.Millisecond, time.Millisecond))
	if err != nil {
		panic(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out := make(chan msgin.Delivery, 4)
	done := make(chan error, 1)
	go func() { done <- client.Stream(ctx, out) }()

	for i := 0; i < 2; i++ {
		d := <-out
		name, _ := d.Msg.Headers().String(msghttp.HeaderSSEEventName)
		fmt.Printf("event=%s data=%s\n", name, d.Msg.Payload())
	}

	if err := <-done; err != nil {
		panic(err)
	}

	// Output:
	// event=greeting data=hello
	// event=greeting data=world
}
