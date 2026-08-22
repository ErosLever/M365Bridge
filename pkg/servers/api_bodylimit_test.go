package servers

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/KilimcininKorOglu/M365Bridge/pkg/models"
)

func bodyLimitServer() *APIServer {
	return &APIServer{config: &models.Config{}}
}

// A body past the cap must be refused rather than buffered whole, and it must
// carry its own code so a client can tell it from a malformed body.
func TestOversizeRequestBodyIsRefusedWithItsOwnCode(t *testing.T) {
	api := bodyLimitServer()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(strings.Repeat("x", 64)))

	limitRequestBody(recorder, request, 16)
	buffer := make([]byte, 64)
	_, err := request.Body.Read(buffer)
	for err == nil {
		_, err = request.Body.Read(buffer)
	}

	if _, ok := errors.AsType[*http.MaxBytesError](err); !ok {
		t.Fatalf("read error = %v, want a MaxBytesError", err)
	}

	api.sendRequestBodyError(recorder, err)

	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want 413", recorder.Code)
	}
	var body struct {
		Error struct {
			Type string `json:"type"`
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("cannot decode the error body: %v", err)
	}
	if body.Error.Code != requestTooLargeCode {
		t.Errorf("code = %q, want %q", body.Error.Code, requestTooLargeCode)
	}
	if body.Error.Type != "invalid_request_error" {
		t.Errorf("type = %q, want invalid_request_error", body.Error.Type)
	}
}

// A malformed body is still a 400, so the new status does not swallow the old
// meaning.
func TestMalformedRequestBodyStaysABadRequest(t *testing.T) {
	api := bodyLimitServer()
	recorder := httptest.NewRecorder()

	api.sendRequestBodyError(recorder, errors.New("unexpected EOF"))

	if recorder.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", recorder.Code)
	}
}

// A body under the cap must pass through untouched.
func TestRequestBodyUnderTheLimitIsReadWhole(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt5.5-reasoning"}`))

	limitRequestBody(recorder, request, requestBodyMax)

	var decoded map[string]any
	if err := json.NewDecoder(request.Body).Decode(&decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded["model"] != "gpt5.5-reasoning" {
		t.Errorf("decoded = %#v", decoded)
	}
}
