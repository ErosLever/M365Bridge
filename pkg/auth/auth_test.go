package auth

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// newCountingTokenEndpoint returns a token endpoint that records every
// presented refresh token and rotates it on each redemption, mimicking Entra's
// single-use refresh token behaviour.
func newCountingTokenEndpoint(t *testing.T, latency time.Duration) (*httptest.Server, func() []string) {
	t.Helper()

	var mu sync.Mutex
	var presented []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse form: %v", err)
			return
		}
		mu.Lock()
		presented = append(presented, r.Form.Get("refresh_token"))
		n := len(presented)
		mu.Unlock()

		// Keep the exchange in flight long enough for other callers to pile up.
		time.Sleep(latency)

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  fmt.Sprintf("access-%d", n),
			"refresh_token": fmt.Sprintf("rotated-%d", n),
			"expires_in":    3600,
		})
	}))
	t.Cleanup(srv.Close)

	return srv, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), presented...)
	}
}

// newTestTokenManager builds a TokenManager pointed at a stub token endpoint
// inside the test's temporary working directory.
func newTestTokenManager(t *testing.T, tokenURL string) *TokenManager {
	t.Helper()
	tm := NewTokenManager("tenant", "client", "scope", "data/tokens/rt.txt", "data/tokens/cache.json")
	tm.tokenURL = tokenURL
	if err := tm.writeRefreshToken("initial-token"); err != nil {
		t.Fatalf("seed refresh token: %v", err)
	}
	return tm
}

// TestConcurrentGetRedeemsRefreshTokenOnce guards the single-use refresh token.
// Without serialization every concurrent caller redeems the same token, and
// Entra invalidates it after the first redemption, which breaks authentication.
func TestConcurrentGetRedeemsRefreshTokenOnce(t *testing.T) {
	useTemporaryWorkingDirectory(t)
	srv, presented := newCountingTokenEndpoint(t, 50*time.Millisecond)
	tm := newTestTokenManager(t, srv.URL)

	const callers = 8
	tokens := make([]string, callers)
	errs := make([]error, callers)

	var wg sync.WaitGroup
	for i := range callers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			tokens[i], errs[i] = tm.Get()
		}(i)
	}
	wg.Wait()

	redemptions := presented()
	if len(redemptions) != 1 {
		t.Fatalf("refresh token redeemed %d times (%v), want exactly 1", len(redemptions), redemptions)
	}
	if redemptions[0] != "initial-token" {
		t.Fatalf("redeemed %q, want the seeded refresh token", redemptions[0])
	}
	for i := range callers {
		if errs[i] != nil {
			t.Fatalf("caller %d failed: %v", i, errs[i])
		}
		if tokens[i] != tokens[0] {
			t.Fatalf("caller %d got %q, want the shared token %q", i, tokens[i], tokens[0])
		}
	}
}

// TestConcurrentRefreshSerializesRedemptions covers the background refresher
// path, where Refresh() is called directly and must never overlap itself.
func TestConcurrentRefreshSerializesRedemptions(t *testing.T) {
	useTemporaryWorkingDirectory(t)
	srv, presented := newCountingTokenEndpoint(t, 20*time.Millisecond)
	tm := newTestTokenManager(t, srv.URL)

	const callers = 4
	var wg sync.WaitGroup
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := tm.Refresh(); err != nil {
				t.Errorf("refresh failed: %v", err)
			}
		}()
	}
	wg.Wait()

	// Every forced refresh redeems, but each must present the token the previous
	// redemption rotated to instead of reusing an already-consumed value.
	redemptions := presented()
	if len(redemptions) != callers {
		t.Fatalf("got %d redemptions, want %d", len(redemptions), callers)
	}
	seen := make(map[string]bool, len(redemptions))
	for i, token := range redemptions {
		if seen[token] {
			t.Fatalf("redemption %d reused refresh token %q", i, token)
		}
		seen[token] = true
	}
}
