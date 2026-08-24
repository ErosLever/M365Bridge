package auth

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

func TestTokenCacheIsEncryptedAtRest(t *testing.T) {
	useTemporaryWorkingDirectory(t)
	tm := NewTokenManager("tenant", "client", "scope", "data/tokens/rt.txt", "data/tokens/cache.json")

	cache := TokenCache{AccessToken: "sensitive-access-token", ExpiresAt: time.Now().Add(time.Hour).Unix()}
	if err := tm.writeCache(cache); err != nil {
		t.Fatalf("write cache: %v", err)
	}

	data, err := os.ReadFile(tm.cacheFile)
	if err != nil {
		t.Fatalf("read cache file: %v", err)
	}
	if strings.Contains(string(data), "sensitive-access-token") || json.Valid(data) {
		t.Fatal("token cache is stored in the clear")
	}

	token, err := tm.loadFromCache()
	if err != nil {
		t.Fatalf("load from cache: %v", err)
	}
	if token != "sensitive-access-token" {
		t.Fatalf("token = %q, want the cached access token", token)
	}
}

func TestTokenCacheMigratesLegacyPlaintext(t *testing.T) {
	useTemporaryWorkingDirectory(t)
	tm := NewTokenManager("tenant", "client", "scope", "data/tokens/rt.txt", "data/tokens/cache.json")

	legacy := TokenCache{AccessToken: "legacy-access-token", ExpiresAt: time.Now().Add(time.Hour).Unix()}
	data, err := json.Marshal(legacy)
	if err != nil {
		t.Fatalf("marshal legacy cache: %v", err)
	}
	if err := os.MkdirAll("data/tokens", 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(tm.cacheFile, data, 0600); err != nil {
		t.Fatalf("write legacy cache: %v", err)
	}

	token, err := tm.loadFromCache()
	if err != nil {
		t.Fatalf("load legacy plaintext cache: %v", err)
	}
	if token != "legacy-access-token" {
		t.Fatalf("token = %q, want the legacy access token", token)
	}
}

func TestDesignerTokenCacheIsEncryptedAtRest(t *testing.T) {
	useTemporaryWorkingDirectory(t)
	if err := os.MkdirAll("data/tokens", 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	cache := designerTokenCache{AccessToken: "sensitive-designer-token", ExpiresAt: time.Now().Add(time.Hour).Unix()}
	if err := writeEncryptedJSON(designerTokenCacheFile, cache); err != nil {
		t.Fatalf("write designer cache: %v", err)
	}

	data, err := os.ReadFile(designerTokenCacheFile)
	if err != nil {
		t.Fatalf("read designer cache file: %v", err)
	}
	if strings.Contains(string(data), "sensitive-designer-token") || json.Valid(data) {
		t.Fatal("designer token cache is stored in the clear")
	}

	token, ok := readDesignerTokenCache()
	if !ok {
		t.Fatal("readDesignerTokenCache reported no valid cache")
	}
	if token != "sensitive-designer-token" {
		t.Fatalf("token = %q, want the cached designer access token", token)
	}
}
