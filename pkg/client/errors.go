package client

import (
	"errors"
	"fmt"
	"net/http"
)

// UpstreamError carries the HTTP status the M365 backend answered with.
//
// The WebSocket dial and the file upload both fail through HTTP, and their
// status separates cases the caller must handle differently: a throttled
// account, an expired token, and a backend outage all reach this package as
// "the request did not go through". Without the status they collapse into one
// opaque failure and the HTTP layer can only report 500.
//
// The message is written for the server log. It names the operation and the
// wrapped transport error, so it must never be sent to an API client.
type UpstreamError struct {
	// Op names the failed operation, for example "dial" or "upload".
	Op string
	// Status is the HTTP status the backend answered with, or zero when the
	// request never reached a response.
	Status int
	// Err is the underlying transport or protocol error.
	Err error
}

// Error implements the error interface.
func (e *UpstreamError) Error() string {
	if e.Status == 0 {
		return fmt.Sprintf("m365 %s failed: %v", e.Op, e.Err)
	}
	return fmt.Sprintf("m365 %s failed with status %d (%s): %v",
		e.Op, e.Status, http.StatusText(e.Status), e.Err)
}

// Unwrap exposes the underlying error to errors.Is and errors.As.
func (e *UpstreamError) Unwrap() error { return e.Err }

// UpstreamStatus reports the HTTP status carried by an UpstreamError anywhere
// in the error chain. The second result is false when the chain holds no
// upstream status, which keeps a real zero status distinguishable from an
// unrelated error.
func UpstreamStatus(err error) (int, bool) {
	if upstream, ok := errors.AsType[*UpstreamError](err); ok {
		return upstream.Status, true
	}
	return 0, false
}
