package servers

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/KilimcininKorOglu/M365Bridge/pkg/auth"
	"github.com/KilimcininKorOglu/M365Bridge/pkg/models"
)

func encryptedProvisioningBody(t *testing.T, secret string, issuedAt time.Time, requestID string, cookies []auth.SSOCookie) string {
	t.Helper()
	plaintext, err := json.Marshal(map[string]any{
		"cookies":    cookies,
		"issued_at":  issuedAt.UnixMilli(),
		"request_id": requestID,
	})
	if err != nil {
		t.Fatalf("marshal provisioning payload: %v", err)
	}
	key := sha256.Sum256([]byte(secret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		t.Fatalf("create cipher: %v", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatalf("create GCM: %v", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	for i := range nonce {
		nonce[i] = byte(i + 1)
	}
	ciphertext := gcm.Seal(nil, nonce, plaintext, []byte(provisionAdditionalData))
	envelope, err := json.Marshal(map[string]any{
		"version":    1,
		"nonce":      base64.StdEncoding.EncodeToString(nonce),
		"ciphertext": base64.StdEncoding.EncodeToString(ciphertext),
	})
	if err != nil {
		t.Fatalf("marshal provisioning envelope: %v", err)
	}
	return string(envelope)
}

func TestResolveProvisionSecretGeneratesDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data", "provision-secret")
	secret, err := resolveProvisionSecret(&models.Config{}, path)
	if err != nil {
		t.Fatalf("resolveProvisionSecret() error = %v", err)
	}
	if len(secret) != provisionSecretMinLength {
		t.Fatalf("generated secret length = %d, want %d", len(secret), provisionSecretMinLength)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read generated secret: %v", err)
	}
	if strings.TrimSpace(string(data)) != secret {
		t.Fatal("persisted secret does not match generated secret")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat generated secret: %v", err)
	}
	if got := info.Mode().Perm(); got != 0600 {
		t.Fatalf("generated secret mode = %o, want 600", got)
	}
}

func TestResolveProvisionSecretUsesExistingDefault(t *testing.T) {
	secret := strings.Repeat("d", provisionSecretMinLength)
	path := filepath.Join(t.TempDir(), "provision-secret")
	if err := os.WriteFile(path, []byte(secret+"\n"), 0600); err != nil {
		t.Fatalf("write default secret: %v", err)
	}
	got, err := resolveProvisionSecret(&models.Config{}, path)
	if err != nil {
		t.Fatalf("resolveProvisionSecret() error = %v", err)
	}
	if got != secret {
		t.Fatalf("resolved secret does not match existing default")
	}
}

func TestResolveProvisionSecretEnvironmentAvoidsDefaultFile(t *testing.T) {
	secret := strings.Repeat("e", provisionSecretMinLength)
	path := filepath.Join(t.TempDir(), "provision-secret")
	got, err := resolveProvisionSecret(&models.Config{ProvisionSecret: secret}, path)
	if err != nil {
		t.Fatalf("resolveProvisionSecret() error = %v", err)
	}
	if got != secret {
		t.Fatal("resolved secret does not match environment secret")
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("default secret file was created: %v", err)
	}
}

func TestResolveProvisionSecretRejectsInvalidFiles(t *testing.T) {
	for _, test := range []struct {
		name    string
		content string
	}{
		{name: "empty"},
		{name: "short", content: strings.Repeat("x", provisionSecretMinLength-1)},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "provision-secret")
			if err := os.WriteFile(path, []byte(test.content), 0600); err != nil {
				t.Fatalf("write secret file: %v", err)
			}
			if _, err := resolveProvisionSecret(&models.Config{}, path); err == nil {
				t.Fatal("resolveProvisionSecret() accepted an invalid persisted secret")
			}
		})
	}
}

func TestResolveProvisionSecretExplicitMissingFileFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing-secret")
	_, err := resolveProvisionSecret(&models.Config{
		ProvisionSecret:     strings.Repeat("e", provisionSecretMinLength),
		ProvisionSecretFile: path,
	}, filepath.Join(t.TempDir(), "default-secret"))
	if err == nil {
		t.Fatal("resolveProvisionSecret() ignored a missing explicit secret file")
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
	wantKey := sha256.Sum256([]byte(fileSecret))
	if got := string(handler.secret); got != string(wantKey[:]) {
		t.Fatalf("handler key does not match the secret loaded from file")
	}
}

func TestNewProvisioningHandlerValidatesExtensionOrigins(t *testing.T) {
	valid := []string{
		"*",
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

func TestProvisioningHandlerOptionalAndWildcardOrigins(t *testing.T) {
	secret := strings.Repeat("s", provisionSecretMinLength)

	for _, test := range []struct {
		name    string
		origins []string
	}{
		{name: "unset"},
		{name: "wildcard", origins: []string{"*"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			handler, err := newProvisioningHandler(&models.Config{
				ProvisionSecret:  secret,
				ProvisionOrigins: test.origins,
			}, func([]auth.SSOCookie) error { return nil })
			if err != nil {
				t.Fatalf("create handler: %v", err)
			}

			request := httptest.NewRequest(http.MethodOptions, "/provision/v1/session", nil)
			request.Header.Set("Origin", "https://example.com")
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)

			if recorder.Code != http.StatusNoContent {
				t.Fatalf("status = %d, want %d: %s", recorder.Code, http.StatusNoContent, recorder.Body.String())
			}
			if got := recorder.Header().Get("Access-Control-Allow-Origin"); got != "*" {
				t.Fatalf("Access-Control-Allow-Origin = %q, want *", got)
			}
		})
	}
}

func TestProvisioningHandlerEncryptedPayload(t *testing.T) {
	const allowedOrigin = "chrome-extension://abcdefghijklmnop"
	secret := strings.Repeat("s", provisionSecretMinLength)
	var provisioned []auth.SSOCookie
	handler, err := newProvisioningHandler(&models.Config{
		ProvisionSecret:  secret,
		ProvisionOrigins: []string{allowedOrigin},
	}, func(cookies []auth.SSOCookie) error {
		provisioned = cookies
		return nil
	})
	if err != nil {
		t.Fatalf("create handler: %v", err)
	}

	cookies := []auth.SSOCookie{{Name: "ESTSAUTH", Value: "sensitive-cookie"}}
	body := encryptedProvisioningBody(t, secret, time.Now(), "request-success", cookies)
	request := httptest.NewRequest(http.MethodPost, "/provision/v1/session", strings.NewReader(body))
	request.Header.Set("Origin", allowedOrigin)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if len(provisioned) != 1 || provisioned[0].Value != "sensitive-cookie" {
		t.Fatalf("provisioned cookies = %#v", provisioned)
	}
	if strings.Contains(body, "sensitive-cookie") || strings.Contains(body, secret) {
		t.Fatal("encrypted envelope exposed cookie or provisioning secret")
	}

	for _, test := range []struct {
		name string
		body string
	}{
		{"wrong secret", encryptedProvisioningBody(t, strings.Repeat("x", provisionSecretMinLength), time.Now(), "request-wrong-key", cookies)},
		{"stale", encryptedProvisioningBody(t, secret, time.Now().Add(-provisionFreshnessWindow-time.Second), "request-stale", cookies)},
		{"replay", body},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/provision/v1/session", strings.NewReader(test.body))
			request.Header.Set("Origin", allowedOrigin)
			request.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
			}
		})
	}
}

func TestProvisioningHandlerClassifiesProvisioningFailures(t *testing.T) {
	const allowedOrigin = "chrome-extension://abcdefghijklmnop"
	secret := strings.Repeat("s", provisionSecretMinLength)
	cookies := []auth.SSOCookie{{Name: "ESTSAUTH", Value: "sensitive-cookie"}}

	for _, test := range []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{"invalid cookies", fmt.Errorf("wrapped: %w", auth.ErrInvalidSSOCookies), http.StatusBadRequest, "invalid_request"},
		{"session validation", errors.New("validate primary SSO session"), http.StatusBadGateway, "session_validation_failed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			handler, err := newProvisioningHandler(&models.Config{
				ProvisionSecret:  secret,
				ProvisionOrigins: []string{allowedOrigin},
			}, func([]auth.SSOCookie) error { return test.err })
			if err != nil {
				t.Fatalf("create handler: %v", err)
			}

			body := encryptedProvisioningBody(t, secret, time.Now(), "request-"+test.name, cookies)
			request := httptest.NewRequest(http.MethodPost, "/provision/v1/session", strings.NewReader(body))
			request.Header.Set("Origin", allowedOrigin)
			request.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)

			if recorder.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d: %s", recorder.Code, test.wantStatus, recorder.Body.String())
			}
			if !strings.Contains(recorder.Body.String(), test.wantCode) {
				t.Fatalf("response does not contain %q: %s", test.wantCode, recorder.Body.String())
			}
		})
	}
}
