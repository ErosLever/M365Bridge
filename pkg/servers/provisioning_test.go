package servers

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KilimcininKorOglu/M365Bridge/pkg/auth"
	"github.com/KilimcininKorOglu/M365Bridge/pkg/models"
)

func TestNewProvisioningHandlerDisabledWithoutSecret(t *testing.T) {
	handler, err := newProvisioningHandler(&models.Config{}, func([]auth.SSOCookie) error { return nil })
	if err != nil {
		t.Fatalf("newProvisioningHandler() error = %v", err)
	}
	if handler.enabled {
		t.Fatal("newProvisioningHandler() enabled provisioning without a secret")
	}
}

func TestNewProvisioningHandlerRejectsShortSecret(t *testing.T) {
	_, err := newProvisioningHandler(&models.Config{ProvisionSecret: strings.Repeat("x", provisionSecretMinLength-1)}, func([]auth.SSOCookie) error { return nil })
	if err == nil {
		t.Fatal("newProvisioningHandler() accepted a secret below the minimum length")
	}
}

func TestNewProvisioningHandlerSecretFileTakesPrecedence(t *testing.T) {
	fileSecret := strings.Repeat("f", provisionSecretMinLength)
	path := filepath.Join(t.TempDir(), "provision-secret")
	if err := os.WriteFile(path, []byte(fileSecret+"\n"), 0600); err != nil {
		t.Fatalf("write secret file: %v", err)
	}

	handler, err := newProvisioningHandler(&models.Config{
		ProvisionSecret:     strings.Repeat("e", provisionSecretMinLength),
		ProvisionSecretFile: path,
	}, func([]auth.SSOCookie) error { return nil })
	if err != nil {
		t.Fatalf("newProvisioningHandler() error = %v", err)
	}
	if !handler.enabled {
		t.Fatal("newProvisioningHandler() did not enable provisioning with a valid file secret")
	}
	if got := string(handler.secret); got != fileSecret {
		t.Fatalf("handler secret = %q, want secret loaded from file", got)
	}
}

func TestNewProvisioningHandlerValidatesExtensionOrigins(t *testing.T) {
	valid := []string{
		"chrome-extension://abcdefghijklmnop",
		"moz-extension://12345678-1234-1234-1234-123456789abc",
	}
	for _, origin := range valid {
		_, err := newProvisioningHandler(&models.Config{
			ProvisionSecret:  strings.Repeat("s", provisionSecretMinLength),
			ProvisionOrigins: []string{origin},
		}, func([]auth.SSOCookie) error { return nil })
		if err != nil {
			t.Errorf("newProvisioningHandler() rejected valid origin %q: %v", origin, err)
		}
	}

	invalid := []string{
		"https://example.com",
		"chrome-extension://id/path",
		"moz-extension://id?query=value",
		"chrome-extension://id\r\nX-Injected: value",
	}
	for _, origin := range invalid {
		_, err := newProvisioningHandler(&models.Config{
			ProvisionSecret:  strings.Repeat("s", provisionSecretMinLength),
			ProvisionOrigins: []string{origin},
		}, func([]auth.SSOCookie) error { return nil })
		if err == nil {
			t.Errorf("newProvisioningHandler() accepted invalid origin %q", origin)
		}
	}
}

func TestProvisioningHandlerHTTPBoundary(t *testing.T) {
	const allowedOrigin = "chrome-extension://abcdefghijklmnop"
	secret := strings.Repeat("s", provisionSecretMinLength)

	disabled, err := newProvisioningHandler(&models.Config{}, func([]auth.SSOCookie) error { return nil })
	if err != nil {
		t.Fatalf("create disabled handler: %v", err)
	}
	recorder := httptest.NewRecorder()
	disabled.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/provision/v1/session", nil))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("disabled handler status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
	if recorder.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("disabled handler response is missing Cache-Control: no-store")
	}

	handler, err := newProvisioningHandler(&models.Config{
		ProvisionSecret:  secret,
		ProvisionOrigins: []string{allowedOrigin},
	}, func([]auth.SSOCookie) error { return nil })
	if err != nil {
		t.Fatalf("create enabled handler: %v", err)
	}

	t.Run("method", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/provision/v1/session", nil))
		if recorder.Code != http.StatusMethodNotAllowed {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusMethodNotAllowed)
		}
		if got := recorder.Header().Get("Allow"); got != "POST, OPTIONS" {
			t.Fatalf("Allow = %q, want POST, OPTIONS", got)
		}
	})

	t.Run("content type", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodPost, "/provision/v1/session", strings.NewReader("{}"))
		request.Header.Set("Content-Type", "text/plain")
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusUnsupportedMediaType {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnsupportedMediaType)
		}
	})

	t.Run("allowed preflight", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodOptions, "/provision/v1/session", nil)
		request.Header.Set("Origin", allowedOrigin)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNoContent)
		}
		if got := recorder.Header().Get("Access-Control-Allow-Origin"); got != allowedOrigin {
			t.Fatalf("Access-Control-Allow-Origin = %q, want %q", got, allowedOrigin)
		}
		if got := recorder.Header().Get("Access-Control-Allow-Methods"); got != http.MethodPost {
			t.Fatalf("Access-Control-Allow-Methods = %q, want POST", got)
		}
	})

	t.Run("denied origin", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodPost, "/provision/v1/session", strings.NewReader("{}"))
		request.Header.Set("Origin", "chrome-extension://untrusted")
		request.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusForbidden)
		}
		if got := recorder.Header().Get("Access-Control-Allow-Origin"); got != "" {
			t.Fatalf("denied origin was reflected as %q", got)
		}
	})
}
