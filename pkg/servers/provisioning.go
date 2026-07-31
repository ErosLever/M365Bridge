package servers

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/KilimcininKorOglu/M365Bridge/pkg/auth"
	"github.com/KilimcininKorOglu/M365Bridge/pkg/logging"
	"github.com/KilimcininKorOglu/M365Bridge/pkg/models"
)

const (
	provisionBodyLimit       = 40 * 1024
	provisionSecretMinLength = 32
	provisionFailureLimit    = 5
	provisionFailureWindow   = time.Minute
)

type provisionFailure struct {
	count int
	since time.Time
}

type provisioningHandler struct {
	enabled   bool
	secret    []byte
	origins   map[string]struct{}
	provision func([]auth.SSOCookie) error
	mu        sync.Mutex
	failures  map[string]provisionFailure
}

func newProvisioningHandler(config *models.Config, provision func([]auth.SSOCookie) error) (*provisioningHandler, error) {
	handler := &provisioningHandler{
		origins:   make(map[string]struct{}, len(config.ProvisionOrigins)),
		provision: provision,
		failures:  make(map[string]provisionFailure),
	}
	for _, origin := range config.ProvisionOrigins {
		if !validExtensionOrigin(origin) {
			return nil, fmt.Errorf("invalid M365_PROVISION_ORIGINS entry")
		}
		handler.origins[origin] = struct{}{}
	}

	secret := config.ProvisionSecret
	if config.ProvisionSecretFile != "" {
		data, err := os.ReadFile(config.ProvisionSecretFile)
		if err != nil {
			return nil, fmt.Errorf("read provisioning secret file: %w", err)
		}
		secret = strings.TrimSpace(string(data))
	}
	if secret == "" {
		return handler, nil
	}
	if len(secret) < provisionSecretMinLength {
		return nil, fmt.Errorf("provisioning secret must contain at least %d bytes", provisionSecretMinLength)
	}

	handler.enabled = true
	handler.secret = []byte(secret)
	return handler, nil
}

func validExtensionOrigin(origin string) bool {
	var extensionID string
	switch {
	case strings.HasPrefix(origin, "chrome-extension://"):
		extensionID = strings.TrimPrefix(origin, "chrome-extension://")
	case strings.HasPrefix(origin, "moz-extension://"):
		extensionID = strings.TrimPrefix(origin, "moz-extension://")
	default:
		return false
	}
	return extensionID != "" && !strings.ContainsAny(extensionID, "/\r\n?#")
}

func (handler *provisioningHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if !handler.enabled {
		handler.sendError(w, http.StatusNotFound, "not_found")
		return
	}

	origin := r.Header.Get("Origin")
	if origin != "" {
		if _, allowed := handler.origins[origin]; !allowed {
			handler.sendError(w, http.StatusForbidden, "origin_not_allowed")
			return
		}
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Vary", "Origin")
	}

	if r.Method == http.MethodOptions {
		if origin == "" {
			handler.sendError(w, http.StatusForbidden, "origin_not_allowed")
			return
		}
		w.Header().Set("Access-Control-Allow-Methods", http.MethodPost)
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		w.Header().Set("Access-Control-Max-Age", "600")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST, OPTIONS")
		handler.sendError(w, http.StatusMethodNotAllowed, "method_not_allowed")
		return
	}
	if handler.rateLimited(r.RemoteAddr) {
		handler.sendError(w, http.StatusTooManyRequests, "rate_limited")
		return
	}
	if !strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
		handler.sendError(w, http.StatusUnsupportedMediaType, "invalid_content_type")
		return
	}

	provided := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	if len(provided) != len(handler.secret) || subtle.ConstantTimeCompare([]byte(provided), handler.secret) != 1 {
		handler.recordFailure(r.RemoteAddr)
		logging.Warnf("Provisioning authorization failed from %s", r.RemoteAddr)
		handler.sendError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var request struct {
		Cookies []auth.SSOCookie `json:"cookies"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, provisionBodyLimit))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		handler.sendError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		handler.sendError(w, http.StatusBadRequest, "invalid_request")
		return
	}

	if err := handler.provision(request.Cookies); err != nil {
		logging.Warn("M365 browser session provisioning failed")
		if errors.Is(err, auth.ErrInvalidSSOCookies) {
			handler.sendError(w, http.StatusBadRequest, "invalid_request")
			return
		}
		handler.sendError(w, http.StatusBadGateway, "session_validation_failed")
		return
	}

	handler.clearFailures(r.RemoteAddr)
	logging.Info("M365 browser session provisioned successfully")
	handler.sendJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (handler *provisioningHandler) rateLimited(remote string) bool {
	handler.mu.Lock()
	defer handler.mu.Unlock()
	failure, exists := handler.failures[remote]
	if !exists || time.Since(failure.since) >= provisionFailureWindow {
		if exists {
			delete(handler.failures, remote)
		}
		return false
	}
	return failure.count >= provisionFailureLimit
}

func (handler *provisioningHandler) recordFailure(remote string) {
	handler.mu.Lock()
	defer handler.mu.Unlock()
	failure := handler.failures[remote]
	if failure.since.IsZero() || time.Since(failure.since) >= provisionFailureWindow {
		failure = provisionFailure{since: time.Now()}
	}
	failure.count++
	handler.failures[remote] = failure
}

func (handler *provisioningHandler) clearFailures(remote string) {
	handler.mu.Lock()
	delete(handler.failures, remote)
	handler.mu.Unlock()
}

func (handler *provisioningHandler) sendError(w http.ResponseWriter, status int, code string) {
	handler.sendJSON(w, status, map[string]any{"error": map[string]string{"code": code}})
}

func (handler *provisioningHandler) sendJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
