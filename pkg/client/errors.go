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

// TurnFailedError reports a turn the backend itself marked as failed.
//
// The completion frame carries the verdict in `result.value` and `turnState`,
// and a failed turn sends no answer message at all. Ignoring the verdict turned
// such a turn into an empty HTTP 200, so a client saw a blank answer instead of
// a failure and could not tell the two apart. A dead `tone` fails this way on
// every request.
//
// Unlike UpstreamError the message carries no transport detail, only the
// backend's own wording, so it is safe to show a client.
type TurnFailedError struct {
	// Value is `result.value`, for example "InternalError".
	Value string
	// TurnState is `item.turnState`, for example "Failed".
	TurnState string
	// Message is `result.message`, the backend's own explanation.
	Message string
}

// Error implements the error interface.
func (e *TurnFailedError) Error() string {
	msg := fmt.Sprintf("m365 turn failed: %s", e.Value)
	if e.TurnState != "" {
		msg += fmt.Sprintf(" (turnState %s)", e.TurnState)
	}
	if e.Message != "" {
		msg += ": " + e.Message
	}
	return msg
}

// TurnFailure reports the backend's verdict when a TurnFailedError appears
// anywhere in the error chain.
func TurnFailure(err error) (*TurnFailedError, bool) {
	return errors.AsType[*TurnFailedError](err)
}
