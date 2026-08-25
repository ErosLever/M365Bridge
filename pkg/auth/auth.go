// Package auth provides token management and OAuth2 authentication for M365 Copilot.
// It handles access token caching, refresh token storage, and token refresh logic.
package auth

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/KilimcininKorOglu/M365Bridge/pkg/crypto"
	"github.com/KilimcininKorOglu/M365Bridge/pkg/logging"
)

var (
	// ErrTokenNotFound is returned when the refresh token file is empty or missing.
	ErrTokenNotFound = errors.New("refresh token not found")
	// ErrRefreshFailed is returned when token refresh fails.
	ErrRefreshFailed = errors.New("token refresh failed")
)

const (
	// tokenURLTemplate is the OAuth2 token endpoint URL template. It is a public
	// Microsoft endpoint and carries no secret.
	// #nosec G101
	tokenURLTemplate = "https://login.microsoftonline.com/%s/oauth2/v2.0/token"
	// cacheExpiryBuffer is the time buffer before token expiry to trigger refresh.
	cacheExpiryBuffer = 60 * time.Second
	// tokenResponseMax caps a token endpoint response. The body is a small JSON
	// object, so anything near this size is a redirected or hostile endpoint
	// rather than a token, and reading it whole would hold it all in memory.
	tokenResponseMax = 1 << 20
	// authPageMax caps a sign-in page read while following redirects. Those
	// pages are HTML and much larger than a token response.
	authPageMax = 4 << 20
)

// TokenCache represents the cached access token data.
type TokenCache struct {
	AccessToken string `json:"access_token"`
	ExpiresAt   int64  `json:"expires_at"`
}

// TokenManager handles OAuth2 token lifecycle management.
//
// Refresh tokens are single-use: Entra rotates them on every redemption and
// invalidates the previous value. The background refresher and every request
// path can both reach a refresh, so all redemption paths are serialized by
// mutexes. refreshMu guards the primary refresh token and access-token cache;
// designerMu guards the separate designerapp broker refresh token and its
// cache. Both are held across the network exchange so two callers can never
// redeem the same refresh token concurrently.
type TokenManager struct {
	identityMu             sync.RWMutex
	tenant                 string
	clientID               string
	scope                  string
	refreshFile            string
	cacheFile              string
	tokenURL               string
	provisionAuthority     string
	userOID                string
	ssoCookiesMu           sync.RWMutex
	ssoCookies             *SSOCookieStore
	designerTokenRequest   func(string) (string, int, error)
	brokerTokenAcquisition func() (string, error)
	refreshMu              sync.Mutex
	designerMu             sync.Mutex
}

// NewTokenManager creates a new TokenManager instance.
func NewTokenManager(tenant, clientID, scope, refreshFile, cacheFile string) *TokenManager {
	return &TokenManager{
		tenant:             tenant,
		clientID:           clientID,
		scope:              scope,
		refreshFile:        refreshFile,
		cacheFile:          cacheFile,
		tokenURL:           fmt.Sprintf(tokenURLTemplate, tenant),
		provisionAuthority: defaultProvisionAuthority,
	}
}

// SetProvisionAuthority selects the Microsoft identity authority used only for
// the initial browser provisioning flow.
func (tm *TokenManager) SetProvisionAuthority(authority string) error {
	authority = strings.ToLower(strings.TrimSpace(authority))
	if !validProvisionAuthority(authority) {
		return fmt.Errorf("invalid provisioning authority %q: expected organizations, common, or a tenant ID", authority)
	}
	tm.provisionAuthority = authority
	return nil
}

// SetUserOID sets the user object ID for broker token requests.
func (tm *TokenManager) SetUserOID(oid string) {
	tm.identityMu.Lock()
	tm.userOID = oid
	tm.identityMu.Unlock()
}

// Identity returns the current user object ID and tenant ID as one coherent pair.
func (tm *TokenManager) Identity() (string, string) {
	tm.identityMu.RLock()
	defer tm.identityMu.RUnlock()
	return tm.userOID, tm.tenant
}

func (tm *TokenManager) setIdentity(oid, tenant string) {
	tm.identityMu.Lock()
	tm.userOID = oid
	tm.tenant = tenant
	tm.tokenURL = fmt.Sprintf(tokenURLTemplate, tenant)
	tm.identityMu.Unlock()
}

func (tm *TokenManager) currentTokenURL() string {
	tm.identityMu.RLock()
	defer tm.identityMu.RUnlock()
	return tm.tokenURL
}

// applyIdentityFromAccessToken restores the user OID and tenant ID from an
// access token's claims. It is called whenever a cached or freshly-refreshed
// access token is returned, so identity survives a process restart even
// though it is otherwise held only in memory: the encrypted refresh token and
// cache on disk carry everything needed to derive it again, without another
// browser provisioning round trip. Failures are non-fatal — the token is
// still usable for M365 Copilot itself and only the identity-derived
// x-anchormailbox header / chat URL are affected.
func (tm *TokenManager) applyIdentityFromAccessToken(accessToken string) {
	oid, tenant, err := identityFromAccessToken(accessToken)
	if err != nil {
		logging.Debugf("applyIdentityFromAccessToken: %v", err)
		return
	}
	tm.setIdentity(oid, tenant)
}

// Get returns a valid access token, refreshing if necessary.
// Returns cached token if valid, otherwise performs token refresh.
func (tm *TokenManager) Get() (string, error) {
	// Try to load from cache first
	if token, err := tm.loadFromCache(); err == nil {
		logging.Debug("TokenManager.Get: cache hit")
		tm.applyIdentityFromAccessToken(token)
		return token, nil
	}

	logging.Debug("TokenManager.Get: cache miss, refreshing")
	tm.refreshMu.Lock()
	defer tm.refreshMu.Unlock()

	// Re-check the cache under the lock. A concurrent caller may have completed
	// a refresh while this goroutine waited, and redeeming again would burn the
	// rotated refresh token for nothing.
	if token, err := tm.loadFromCache(); err == nil {
		logging.Debug("TokenManager.Get: cache filled while waiting for refresh lock")
		tm.applyIdentityFromAccessToken(token)
		return token, nil
	}

	return tm.refreshLocked()
}

// Refresh exchanges the refresh token for a new access token.
// Updates both the refresh token file and cache file.
func (tm *TokenManager) Refresh() (string, error) {
	tm.refreshMu.Lock()
	defer tm.refreshMu.Unlock()
	return tm.refreshLocked()
}

// refreshLocked performs the refresh token exchange. Callers must hold
// refreshMu, which keeps the single-use refresh token from being redeemed by
// two goroutines at once.
func (tm *TokenManager) refreshLocked() (string, error) {
	logging.Info("TokenManager.Refresh: starting token refresh")
	refreshToken, err := tm.readRefreshToken()
	if err != nil {
		logging.Errorf("TokenManager.Refresh: failed to read refresh token: %v", err)
		return "", err
	}

	data := url.Values{}
	data.Set("client_id", tm.clientID)
	data.Set("refresh_token", refreshToken)
	data.Set("grant_type", "refresh_token")
	data.Set("scope", tm.scope)

	req, err := http.NewRequest("POST", tm.currentTokenURL(), bytes.NewBufferString(data.Encode()))
	if err != nil {
		return "", fmt.Errorf("%w: failed to create request", ErrRefreshFailed)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "https://m365.cloud.microsoft")
	req.Header.Set("User-Agent", "Mozilla/5.0")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrRefreshFailed, err)
	}
	defer func() { _ = resp.Body.Close() }()

	// One extra byte distinguishes "exactly at the limit" from "truncated".
	body, err := io.ReadAll(io.LimitReader(resp.Body, tokenResponseMax+1))
	if err != nil {
		return "", fmt.Errorf("%w: failed to read response", ErrRefreshFailed)
	}
	if len(body) > tokenResponseMax {
		return "", fmt.Errorf("%w: token response exceeds %d bytes", ErrRefreshFailed, tokenResponseMax)
	}

	if resp.StatusCode != http.StatusOK {
		errMsg := string(body)
		// If refresh token expired (AADSTS700084), try SSO cookie re-auth as fallback
		if strings.Contains(errMsg, "AADSTS700084") && tm.hasSSOCookies() {
			logging.Warn("TokenManager.Refresh: refresh token expired (AADSTS700084), falling back to SSO cookie re-auth")
			return tm.reauthWithSSO()
		}
		logging.Errorf("TokenManager.Refresh: token refresh failed status=%d: %s", resp.StatusCode, errMsg[:min(200, len(errMsg))])
		return "", fmt.Errorf("%w: status %d: %s", ErrRefreshFailed, resp.StatusCode, errMsg)
	}

	var result struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		logging.Errorf("TokenManager.Refresh: failed to parse response: %v", err)
		return "", fmt.Errorf("%w: failed to parse response", ErrRefreshFailed)
	}

	// Save new refresh token if provided
	if result.RefreshToken != "" {
		if err := tm.writeRefreshToken(result.RefreshToken); err != nil {
			logging.Errorf("TokenManager.Refresh: failed to save refresh token: %v", err)
			return "", fmt.Errorf("%w: failed to save refresh token", ErrRefreshFailed)
		}
	}

	// Cache access token
	expiresAt := time.Now().Add(time.Duration(result.ExpiresIn) * time.Second)
	cache := TokenCache{
		AccessToken: result.AccessToken,
		ExpiresAt:   expiresAt.Unix(),
	}

	if err := tm.writeCache(cache); err != nil {
		logging.Errorf("TokenManager.Refresh: failed to write cache: %v", err)
		return "", fmt.Errorf("%w: failed to write cache", ErrRefreshFailed)
	}

	tm.applyIdentityFromAccessToken(result.AccessToken)

	logging.Infof("TokenManager.Refresh: success, expires_in=%d expires_at=%s", result.ExpiresIn, expiresAt.Format(time.RFC3339))
	return result.AccessToken, nil
}

// readRefreshToken reads and decrypts the refresh token from file.
func (tm *TokenManager) readRefreshToken() (string, error) {
	data, err := os.ReadFile(tm.refreshFile)
	if err != nil {
		return "", fmt.Errorf("%w: %s", ErrTokenNotFound, tm.refreshFile)
	}

	encrypted := string(data)
	if encrypted == "" {
		return "", ErrTokenNotFound
	}

	// Try to decrypt
	decrypted, err := crypto.Decrypt(encrypted)
	if err != nil {
		// If decryption fails, assume it's plaintext (legacy support)
		return encrypted, nil
	}

	return decrypted, nil
}

// writeRefreshToken encrypts and writes the refresh token to file.
func (tm *TokenManager) writeRefreshToken(token string) error {
	encrypted, err := crypto.Encrypt(token)
	if err != nil {
		return fmt.Errorf("failed to encrypt token: %w", err)
	}

	// Ensure directory exists
	dir := filepath.Dir(tm.refreshFile)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return fmt.Errorf("failed to create directory: %w", err)
		}
	}

	return atomicWriteFile(tm.refreshFile, []byte(encrypted), 0600)
}

// loadFromCache attempts to load a valid access token from cache.
func (tm *TokenManager) loadFromCache() (string, error) {
	var cache TokenCache
	if err := readEncryptedJSON(tm.cacheFile, &cache); err != nil {
		return "", err
	}

	// Check if token is still valid
	if cache.ExpiresAt > time.Now().Add(cacheExpiryBuffer).Unix() {
		return cache.AccessToken, nil
	}

	return "", errors.New("token expired")
}

// writeCache writes the access token cache to file.
//
// The cache holds the access token by design; caching it is the whole point of
// the file. It lives under the gitignored data/ tree, its directory is 0700 and
// the file itself is 0600. The access token inside is encrypted at rest with
// the same key that protects the refresh token, the same way and for the same
// reason: it is a live credential, not just a pointer to one.
func (tm *TokenManager) writeCache(cache TokenCache) error {
	// Ensure directory exists
	dir := filepath.Dir(tm.cacheFile)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return fmt.Errorf("failed to create directory: %w", err)
		}
	}

	return writeEncryptedJSON(tm.cacheFile, cache)
}

// readEncryptedJSON reads a file written by writeEncryptedJSON and decodes it
// into v. A file left over from before caches were encrypted is plain JSON, so
// a decryption failure falls back to decoding the bytes directly.
func readEncryptedJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	if plaintext, err := crypto.Decrypt(string(data)); err == nil {
		data = []byte(plaintext)
	}

	return json.Unmarshal(data, v)
}

// writeEncryptedJSON marshals v to JSON and encrypts it before writing, so a
// cached access token is never stored in the clear.
func writeEncryptedJSON(path string, v any) error {
	// #nosec G117
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}

	encrypted, err := crypto.Encrypt(string(data))
	if err != nil {
		return fmt.Errorf("failed to encrypt cache: %w", err)
	}

	return atomicWriteFile(path, []byte(encrypted), 0600)
}
