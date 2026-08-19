package servers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpenAIErrorTypeMapsEveryStatusClass(t *testing.T) {
	cases := map[int]string{
		http.StatusBadRequest:          "invalid_request_error",
		http.StatusUnauthorized:        "authentication_error",
		http.StatusForbidden:           "authentication_error",
		http.StatusNotFound:            "invalid_request_error",
		http.StatusMethodNotAllowed:    "invalid_request_error",
		http.StatusConflict:            "invalid_request_error",
		http.StatusTooManyRequests:     "rate_limit_error",
		http.StatusInternalServerError: "server_error",
		http.StatusBadGateway:          "server_error",
		http.StatusServiceUnavailable:  "server_error",
		http.StatusGatewayTimeout:      "server_error",
	}
	for status, want := range cases {
		if got := openAIErrorType(status); got != want {
			t.Fatalf("openAIErrorType(%d) = %q, want %q", status, got, want)
		}
	}
}

func TestOpenAIErrorCodeIsASlugNotANumber(t *testing.T) {
	cases := map[int]string{
		http.StatusBadRequest:          "bad_request",
		http.StatusMethodNotAllowed:    "method_not_allowed",
		http.StatusInternalServerError: "internal_server_error",
		http.StatusTooManyRequests:     "too_many_requests",
	}
	for status, want := range cases {
		if got := openAIErrorCode(status); got != want {
			t.Fatalf("openAIErrorCode(%d) = %q, want %q", status, got, want)
		}
	}
	if got := openAIErrorCode(799); got != "error" {
		t.Fatalf("an unnamed status produced %q", got)
	}
}

// decodeErrorBody reads the error envelope as the wire carries it, so a code
// that regressed to a number fails to decode into a string.
func decodeErrorBody(t *testing.T, body []byte) (message, errType, code string) {
	t.Helper()
	var decoded struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
			Code    string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decode error body %s: %v", body, err)
	}
	return decoded.Error.Message, decoded.Error.Type, decoded.Error.Code
}

func TestSendErrorUsesTheOpenAIShape(t *testing.T) {
	api := &APIServer{}
	rec := httptest.NewRecorder()
	api.sendError(rec, http.StatusBadRequest, "Invalid model")

	message, errType, code := decodeErrorBody(t, rec.Body.Bytes())
	if message != "Invalid model" {
		t.Fatalf("message = %q", message)
	}
	if errType != "invalid_request_error" {
		t.Fatalf("type = %q, want the category", errType)
	}
	if code != "bad_request" {
		t.Fatalf("code = %q, want a string slug", code)
	}
}

func TestSendErrorCodeKeepsTheSpecificCode(t *testing.T) {
	api := &APIServer{}
	rec := httptest.NewRecorder()
	api.sendErrorCode(rec, http.StatusTooManyRequests, upstreamThrottledCode, "quota exhausted")

	_, errType, code := decodeErrorBody(t, rec.Body.Bytes())
	if errType != "rate_limit_error" {
		t.Fatalf("type = %q, want the category", errType)
	}
	if code != upstreamThrottledCode {
		t.Fatalf("code = %q, want %q", code, upstreamThrottledCode)
	}
}

func TestContentBlockedErrorCarriesItsCodeInTheCodeField(t *testing.T) {
	api := &APIServer{}
	rec := httptest.NewRecorder()
	api.sendContentBlockedError(rec, "I'm sorry, I can't respond to that.")

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d", rec.Code)
	}
	_, errType, code := decodeErrorBody(t, rec.Body.Bytes())
	if errType != "server_error" {
		t.Fatalf("type = %q, want the category", errType)
	}
	if code != upstreamContentBlockedCode {
		t.Fatalf("code = %q, want %q", code, upstreamContentBlockedCode)
	}
}
