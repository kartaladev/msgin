package msghttp_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/kartaladev/msgin"
	msghttp "github.com/kartaladev/msgin/adapter/http"
)

// ExampleNewSSEServer serves Server-Sent Events over an httptest.Server: a
// client connects, the server registers it, and a single Send fans one event
// out to that subscriber.
//
// Determinism note: http.Get returns only once the response's status line and
// headers have arrived, and ServeHTTP registers the connection (under its
// internal lock) strictly BEFORE it writes and flushes those headers — so by
// the time Get returns, the subscriber is guaranteed already registered and
// Send cannot drop this event. No time.Sleep or retry loop is needed.
func ExampleNewSSEServer() {
	server, err := msghttp.NewSSEServer()
	if err != nil {
		panic(err)
	}

	ts := httptest.NewServer(server)
	defer ts.Close()

	resp, err := http.Get(ts.URL)
	if err != nil {
		panic(err)
	}

	msg := msgin.New[any]([]byte("hello"), msgin.WithID("evt-1"))
	if err := server.Send(context.Background(), msg); err != nil {
		panic(err)
	}

	parser, err := msghttp.NewSSEParser(resp.Body)
	if err != nil {
		panic(err)
	}
	ev, err := parser.Next()
	if err != nil {
		panic(err)
	}
	fmt.Printf("id=%s data=%s\n", ev.ID, ev.Data)

	// Tear down cleanly, in order: disconnect the client, join the server's
	// connection via Close, then stop the httptest server.
	_ = resp.Body.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Close(ctx); err != nil {
		panic(err)
	}

	// Output:
	// id=evt-1 data=hello
}
