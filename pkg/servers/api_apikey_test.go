package servers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/KilimcininKorOglu/M365Bridge/pkg/models"
)

// authStatus runs one request through the middleware and reports the status.
func authStatus(t *testing.T, headers map[string]string) int {
	t.Helper()
	api := &APIServer{config: &models.Config{APIKeys: []string{"good-key"}}}
	handler := api.withAuth(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	recorder := httptest.NewRecorder()
	handler(recorder, request)
	return recorder.Code
}

func TestAuthAcceptsEitherKeyHeader(t *testing.T) {
	// An Anthropic SDK client sends x-api-key under ANTHROPIC_API_KEY and
	// Authorization under ANTHROPIC_AUTH_TOKEN, so both must work.
	cases := map[string]map[string]string{
		"x-api-key":                      {"X-API-Key": "good-key"},
		"authorization bearer":           {"Authorization": "Bearer good-key"},
		"authorization lowercase scheme": {"Authorization": "bearer good-key"},
		"authorization without scheme":   {"Authorization": "good-key"},
	}
	for name, headers := range cases {
		if got := authStatus(t, headers); got != http.StatusOK {
			t.Fatalf("%s: status = %d, want 200", name, got)
		}
	}
}

func TestAuthAcceptsAValidKeyBesideAStaleOne(t *testing.T) {
	// Claude Code can send both headers at once, with x-api-key carrying a
	// stale key from a previous provider. Checking only the first would reject
	// a request that does present a valid key.
	got := authStatus(t, map[string]string{
		"X-API-Key":     "stale-key",
		"Authorization": "Bearer good-key",
	})
	if got != http.StatusOK {
		t.Fatalf("status = %d, want 200 when one of two offered keys is valid", got)
	}
}

func TestAuthRejectsMissingAndWrongKeys(t *testing.T) {
	if got := authStatus(t, nil); got != http.StatusUnauthorized {
		t.Fatalf("no header: status = %d, want 401", got)
	}
	if got := authStatus(t, map[string]string{"X-API-Key": "wrong"}); got != http.StatusUnauthorized {
		t.Fatalf("wrong key: status = %d, want 401", got)
	}
	if got := authStatus(t, map[string]string{"Authorization": "Bearer "}); got != http.StatusUnauthorized {
		t.Fatalf("empty bearer: status = %d, want 401", got)
	}
}
