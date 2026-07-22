package msghttp_test

import (
	"io"
	"net/http"
	"sync/atomic"
)

// This file holds the hermetic HTTP test doubles shared by the O1 (Task 4) and
// O2 (Task 5) tests: a body that records its read/close lifecycle, and a
// function-typed http.RoundTripper for injecting hand-built responses and
// transport errors without any network (Spec 011 §7). The counting sinks and the
// hand-built-response helpers live in classify_test.go and are reused as-is.

// trackingBody is an io.ReadCloser wrapping r that records how many bytes were
// read and whether Close ran. The tests use it to prove Send/Exchange's single
// deferred Close fires on every exit path (INV-7) and that the post-classify
// reuse-drain is bounded by WithMaxResponseBytes (branch 10 / INV-6). Both
// counters are atomic because the read may happen on a transport goroutine.
type trackingBody struct {
	r      io.Reader
	closed atomic.Bool
	read   atomic.Int64
}

func (b *trackingBody) Read(p []byte) (int, error) {
	n, err := b.r.Read(p)
	b.read.Add(int64(n))
	return n, err
}

func (b *trackingBody) Close() error {
	b.closed.Store(true)
	return nil
}

// roundTripperFunc adapts a function to http.RoundTripper so a test can hand
// (*http.Client).Do a hand-built *http.Response or a transport error with no
// network — the hermetic-HTTP requirement (Spec 011 §7). When the function
// returns a non-nil error, (*http.Client).Do wraps it in a *url.Error exactly as
// a real transport failure is, which is what the INV-5 redaction tests rely on.
type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
