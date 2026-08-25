package servers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/KilimcininKorOglu/M365Bridge/pkg/models"
)

func authServer(password string, keys ...string) *APIServer {
	return &APIServer{config: &models.Config{APIKeys: keys, WebUIPassword: password}}
}

// The interface cannot ask for a credential it has not been told about, so the
// mode has to name the gate rather than leave the interface to guess.
func TestAuthModeNamesTheGateToShow(t *testing.T) {
	cases := []struct {
		name     string
		password string
		keys     []string
		want     string
	}{
		{"nothing configured", "", nil, authModeNone},
		{"password only", "letmein", nil, authModePassword},
		{"keys only", "", []string{"k1"}, authModeAPIKey},
		{"password wins over keys", "letmein", []string{"k1"}, authModePassword},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := authServer(tc.password, tc.keys...).authMode(); got != tc.want {
				t.Errorf("authMode() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestHandleAuthModeReportsThePasswordGate(t *testing.T) {
	api := authServer("letmein")
	rec := httptest.NewRecorder()
	api.handleAuthMode(rec, httptest.NewRequest(http.MethodGet, "/v1/auth", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body struct {
		Mode string `json:"mode"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("cannot decode the body: %v", err)
	}
	if body.Mode != authModePassword {
		t.Errorf("mode = %q, want %q", body.Mode, authModePassword)
	}
}

// The password never reaches the response, in any mode.
func TestHandleAuthModeNeverEchoesTheSecret(t *testing.T) {
	api := authServer("letmein", "k1")
	rec := httptest.NewRecorder()
	api.handleAuthMode(rec, httptest.NewRequest(http.MethodGet, "/v1/auth", nil))

	for _, secret := range []string{"letmein", "k1"} {
		if strings.Contains(rec.Body.String(), secret) {
			t.Errorf("the response carries the secret %q", secret)
		}
	}
}

// With a password set but no API key, every route answers 200 without a
// credential, so the interface cannot tell a wrong password from a right one by
// making an ordinary request. This route is what decides it.
func TestAuthVerifyRefusesAWrongPasswordEvenWithTheAPIOpen(t *testing.T) {
	api := authServer("letmein")

	wrong := httptest.NewRequest(http.MethodPost, "/v1/auth/verify", nil)
	wrong.Header.Set("Authorization", "Bearer nope")
	rec := httptest.NewRecorder()
	api.handleAuthVerify(rec, wrong)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("a wrong password got %d, want 401", rec.Code)
	}

	right := httptest.NewRequest(http.MethodPost, "/v1/auth/verify", nil)
	right.Header.Set("Authorization", "Bearer letmein")
	rec = httptest.NewRecorder()
	api.handleAuthVerify(rec, right)
	if rec.Code != http.StatusOK {
		t.Errorf("the password got %d, want 200", rec.Code)
	}
}

func TestAuthVerifyAcceptsAnAPIKeyAndBothHeaders(t *testing.T) {
	api := authServer("", "k1", "k2")

	for _, header := range []struct{ name, value string }{
		{"Authorization", "Bearer k1"},
		{"X-API-Key", "k2"},
	} {
		req := httptest.NewRequest(http.MethodPost, "/v1/auth/verify", nil)
		req.Header.Set(header.name, header.value)
		rec := httptest.NewRecorder()
		api.handleAuthVerify(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("%s: status = %d, want 200", header.name, rec.Code)
		}
	}
}

// With nothing configured there is no credential to check, and the interface
// must not be sent to a gate it can never pass.
func TestAuthVerifyPassesWhenNothingIsConfigured(t *testing.T) {
	api := authServer("")
	rec := httptest.NewRecorder()
	api.handleAuthVerify(rec, httptest.NewRequest(http.MethodPost, "/v1/auth/verify", nil))

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

// The password is a credential the routes behind withAuth accept, which is what
// lets the interface reach them without a session mechanism of its own.
func TestWithAuthAcceptsTheWebUIPassword(t *testing.T) {
	api := authServer("letmein", "k1")
	reached := false
	handler := api.withAuth(func(http.ResponseWriter, *http.Request) { reached = true })

	req := httptest.NewRequest(http.MethodGet, "/v1/sessions", nil)
	req.Header.Set("Authorization", "Bearer letmein")
	handler(httptest.NewRecorder(), req)

	if !reached {
		t.Error("the password did not pass withAuth")
	}
}

// An empty password must never become a credential, or a deployment with keys
// and no password would accept a request that offered an empty string.
func TestAnEmptySecretMatchesNothing(t *testing.T) {
	api := authServer("", "k1")

	if api.isValidAPIKey("") {
		t.Error("an empty token was accepted")
	}
	if secretEqual("", "") {
		t.Error("secretEqual matched two empty secrets")
	}
}

func TestWithAuthStillRefusesAnUnknownCredential(t *testing.T) {
	api := authServer("letmein", "k1")
	reached := false
	handler := api.withAuth(func(http.ResponseWriter, *http.Request) { reached = true })

	req := httptest.NewRequest(http.MethodGet, "/v1/sessions", nil)
	req.Header.Set("Authorization", "Bearer nope")
	rec := httptest.NewRecorder()
	handler(rec, req)

	if reached {
		t.Error("an unknown credential reached the handler")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

// A wrong password tried enough times locks the remote address out of
// /v1/auth/verify, on top of the constant-time comparison the route already
// makes to avoid answering a guess faster than a right one.
func TestAuthVerifyLocksOutRepeatedInvalidCredentials(t *testing.T) {
	api := authServer("letmein")

	wrongAttempt := func() int {
		req := httptest.NewRequest(http.MethodPost, "/v1/auth/verify", nil)
		req.RemoteAddr = "203.0.113.1:12345"
		req.Header.Set("Authorization", "Bearer nope")
		rec := httptest.NewRecorder()
		api.handleAuthVerify(rec, req)
		return rec.Code
	}

	for i := range authFailureLimit {
		if got := wrongAttempt(); got != http.StatusUnauthorized {
			t.Fatalf("attempt %d: status = %d, want 401", i, got)
		}
	}
	if got := wrongAttempt(); got != http.StatusTooManyRequests {
		t.Fatalf("after %d failures: status = %d, want 429", authFailureLimit, got)
	}

	// The right password from a different address is unaffected.
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/verify", nil)
	req.RemoteAddr = "203.0.113.2:12345"
	req.Header.Set("Authorization", "Bearer letmein")
	rec := httptest.NewRecorder()
	api.handleAuthVerify(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("a different address: status = %d, want 200", rec.Code)
	}
}

// A password with no API key gates the interface and leaves the API open, which
// is what an empty key list means everywhere else. This pins that choice so it
// cannot change by accident.
func TestAPasswordAloneLeavesTheAPIOpen(t *testing.T) {
	api := authServer("letmein")
	reached := false
	handler := api.withAuth(func(http.ResponseWriter, *http.Request) { reached = true })

	handler(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/v1/sessions", nil))

	if !reached {
		t.Error("a request with no credential was refused while no API key is configured")
	}
}
