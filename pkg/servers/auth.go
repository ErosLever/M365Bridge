package servers

import (
	"net/http"
	"slices"

	"github.com/KilimcininKorOglu/M365Bridge/pkg/logging"
)

// The browser interface is served without a credential, because the screen that
// asks for one cannot itself require one. It therefore cannot discover what to
// ask for by making a request: with no API key configured every route answers
// 200, so a wrong password would look like a right one.
//
// These two routes close that gap. handleAuthMode reports which gate to show,
// and handleAuthVerify answers whether an offered credential is one this
// gateway accepts. Both are public, and neither discloses anything a caller
// could not already learn by sending an unauthenticated request.

// Auth modes the browser interface switches on.
const (
	// authModeNone means nothing gates the interface or the API.
	authModeNone = "none"
	// authModePassword means the interface asks for M365_WEB_UI_PASSWORD.
	authModePassword = "password"
	// authModeAPIKey means no password is set but API keys are, so the
	// interface asks for a key: without one its every data call is refused.
	authModeAPIKey = "api_key"
)

// authMode reports what the browser interface must ask its user for.
//
// The password wins when both are configured, because it is the credential
// meant for a person while a key is meant for a program.
func (api *APIServer) authMode() string {
	switch {
	case api.config.WebUIPassword != "":
		return authModePassword
	case len(api.config.APIKeys) > 0:
		return authModeAPIKey
	default:
		return authModeNone
	}
}

// handleAuthMode reports which gate the browser interface should show.
func (api *APIServer) handleAuthMode(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		api.handleCORS(w, r)
		return
	}
	if r.Method != http.MethodGet {
		api.sendError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	api.sendJSON(w, http.StatusOK, map[string]any{
		"object": "auth",
		"mode":   api.authMode(),
	})
}

// handleAuthVerify answers whether the offered credential is accepted.
//
// The credential arrives in the same header a client sends its API key in, not
// in a body, so it stays out of request logs and out of anything that records a
// payload.
func (api *APIServer) handleAuthVerify(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		api.handleCORS(w, r)
		return
	}
	if r.Method != http.MethodPost && r.Method != http.MethodGet {
		api.sendError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// With nothing configured there is no credential to check, and the
	// interface opens without a gate.
	if api.authMode() == authModeNone {
		api.sendJSON(w, http.StatusOK, map[string]any{
			"object":        "auth.verification",
			"authenticated": true,
			"mode":          authModeNone,
		})
		return
	}

	if api.authFailures.limited(r.RemoteAddr) {
		logging.Warnf("Auth: %s locked out of /v1/auth/verify after repeated invalid credentials", r.RemoteAddr)
		api.sendError(w, http.StatusTooManyRequests, "Too many invalid credentials; try again later")
		return
	}

	if slices.ContainsFunc(apiKeyCandidates(r), api.isValidAPIKey) {
		api.authFailures.clear(r.RemoteAddr)
		api.sendJSON(w, http.StatusOK, map[string]any{
			"object":        "auth.verification",
			"authenticated": true,
			"mode":          api.authMode(),
		})
		return
	}

	api.authFailures.recordFailure(r.RemoteAddr)
	logging.Warnf("Auth: rejected a credential offered to /v1/auth/verify from %s", r.RemoteAddr)
	api.sendError(w, http.StatusUnauthorized, "Invalid credential")
}
