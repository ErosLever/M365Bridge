package servers

import (
	"net/http"
	"strings"
)

// OpenAI's error body carries a category in "type" and a machine-readable
// string in "code". This service had the two reversed: "type" held the specific
// code and "code" held the HTTP status as a number. Clients written against the
// OpenAI contract read "type" to decide whether to retry, re-authenticate or
// give up, so the reversal made every error look the same to them.
//
// sendError keeps its signature, because eighty call sites pass only a status
// and a message. Both fields are derived from that status instead.

// openAIErrorType maps an HTTP status onto the OpenAI error category.
func openAIErrorType(status int) string {
	switch {
	case status == http.StatusUnauthorized, status == http.StatusForbidden:
		return "authentication_error"
	case status == http.StatusTooManyRequests:
		return "rate_limit_error"
	case status >= 400 && status < 500:
		return "invalid_request_error"
	default:
		return "server_error"
	}
}

// openAIErrorCode derives the default machine-readable code from an HTTP
// status, for example "bad_request" or "method_not_allowed". Callers that have
// a more specific code pass it to sendErrorCode instead.
func openAIErrorCode(status int) string {
	text := http.StatusText(status)
	if text == "" {
		return "error"
	}
	return strings.ToLower(strings.ReplaceAll(text, " ", "_"))
}
