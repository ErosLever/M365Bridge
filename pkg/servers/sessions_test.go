package servers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/KilimcininKorOglu/M365Bridge/pkg/auth"
	"github.com/KilimcininKorOglu/M365Bridge/pkg/models"
)

func TestSetWritesTheRecordFormat(t *testing.T) {
	cache := NewContextCache(t.TempDir())
	cache.Set(sessionKeyPrefix+"dev-test-002", "conv-1")

	if got := cache.Get(sessionKeyPrefix + "dev-test-002"); got != "conv-1" {
		t.Fatalf("Get = %q, want conv-1", got)
	}

	data, err := os.ReadFile(cache.path(sessionKeyPrefix + "dev-test-002"))
	if err != nil {
		t.Fatalf("read cache file: %v", err)
	}
	var record sessionRecord
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatalf("decode cache file %s: %v", data, err)
	}
	if record.SessionID != "dev-test-002" {
		t.Fatalf("session_id = %q", record.SessionID)
	}
	if record.ConversationID != "conv-1" {
		t.Fatalf("conversation_id = %q", record.ConversationID)
	}
	if record.UpdatedAt == 0 {
		t.Fatal("the record carries no timestamp")
	}
}

// Every mapping written before the record format is a bare JSON string. Those
// files are live sessions, so losing them would silently restart every
// conversation.
func TestGetStillReadsTheLegacyFormat(t *testing.T) {
	dir := t.TempDir()
	cache := NewContextCache(dir)
	legacy, _ := json.Marshal("conv-legacy")
	if err := os.WriteFile(cache.path(sessionKeyPrefix+"old-session"), legacy, 0600); err != nil {
		t.Fatalf("write legacy file: %v", err)
	}

	if got := cache.Get(sessionKeyPrefix + "old-session"); got != "conv-legacy" {
		t.Fatalf("Get = %q, want conv-legacy", got)
	}
}

func TestListSortsNewestFirstAndCountsLegacyEntries(t *testing.T) {
	dir := t.TempDir()
	cache := NewContextCache(dir)

	for _, s := range []struct {
		id        string
		conv      string
		updatedAt int64
	}{
		{"a", "conv-a", 100},
		{"b", "conv-b", 300},
		{"c", "conv-c", 200},
	} {
		data, _ := json.Marshal(sessionRecord{SessionID: s.id, ConversationID: s.conv, UpdatedAt: s.updatedAt})
		if err := os.WriteFile(cache.path(sessionKeyPrefix+s.id), data, 0600); err != nil {
			t.Fatalf("write record: %v", err)
		}
	}
	legacy, _ := json.Marshal("conv-legacy")
	for _, name := range []string{"old-1", "old-2"} {
		if err := os.WriteFile(cache.path(sessionKeyPrefix+name), legacy, 0600); err != nil {
			t.Fatalf("write legacy record: %v", err)
		}
	}

	records, legacyCount := cache.List()
	if legacyCount != 2 {
		t.Fatalf("legacy count = %d, want 2", legacyCount)
	}
	if len(records) != 3 {
		t.Fatalf("listed %d records, want 3", len(records))
	}
	for i, want := range []string{"b", "c", "a"} {
		if records[i].SessionID != want {
			t.Fatalf("record %d = %q, want %q (newest first)", i, records[i].SessionID, want)
		}
	}
}

func TestLookupReportsAConversationAndATimestamp(t *testing.T) {
	cache := NewContextCache(t.TempDir())
	cache.Set(sessionKeyPrefix+"dev-test-002", "conv-1")

	record, ok := cache.Lookup("dev-test-002")
	if !ok {
		t.Fatal("Lookup reported a stored session as missing")
	}
	if record.ConversationID != "conv-1" || record.UpdatedAt == 0 {
		t.Fatalf("record = %#v", record)
	}

	if _, ok := cache.Lookup("never-used"); ok {
		t.Fatal("Lookup invented a session that was never stored")
	}
}

// A legacy entry carries no timestamp of its own, so the file modification
// time has to stand in; a zero would read as the epoch.
func TestLookupFallsBackToTheFileTime(t *testing.T) {
	cache := NewContextCache(t.TempDir())
	legacy, _ := json.Marshal("conv-legacy")
	if err := os.WriteFile(cache.path(sessionKeyPrefix+"old-session"), legacy, 0600); err != nil {
		t.Fatalf("write legacy file: %v", err)
	}

	record, ok := cache.Lookup("old-session")
	if !ok {
		t.Fatal("Lookup missed a legacy entry")
	}
	if record.UpdatedAt == 0 {
		t.Fatal("a legacy entry reported no timestamp")
	}
}

// sessionTestServer builds a server whose cache lives in a temp dir, so the
// route tests never touch the real data/cache.
func sessionTestServer(t *testing.T) *APIServer {
	t.Helper()
	return &APIServer{
		config:   &models.Config{},
		ctxCache: NewContextCache(t.TempDir()),
	}
}

func TestSessionsListReportsStoredMappings(t *testing.T) {
	api := sessionTestServer(t)
	api.ctxCache.Set(sessionKeyPrefix+"dev-test-002", "conv-1")

	rec := httptest.NewRecorder()
	api.handleSessions(rec, httptest.NewRequest(http.MethodGet, "/v1/sessions", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var body struct {
		Object string `json:"object"`
		Data   []struct {
			ID             string `json:"id"`
			ConversationID string `json:"conversation_id"`
			UpdatedAt      int64  `json:"updated_at"`
		} `json:"data"`
		LegacyEntries int `json:"legacy_entries"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode %s: %v", rec.Body.Bytes(), err)
	}
	if body.Object != "list" || len(body.Data) != 1 {
		t.Fatalf("body = %s", rec.Body.String())
	}
	if body.Data[0].ID != "dev-test-002" || body.Data[0].ConversationID != "conv-1" {
		t.Fatalf("entry = %#v", body.Data[0])
	}
	if body.LegacyEntries != 0 {
		t.Fatalf("legacy_entries = %d, want 0", body.LegacyEntries)
	}
}

func TestSessionsListRejectsOtherMethods(t *testing.T) {
	api := sessionTestServer(t)
	rec := httptest.NewRecorder()
	api.handleSessions(rec, httptest.NewRequest(http.MethodPost, "/v1/sessions", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

func TestSessionGetReportsAMissingSession(t *testing.T) {
	api := sessionTestServer(t)
	rec := httptest.NewRecorder()
	api.handleSession(rec, httptest.NewRequest(http.MethodGet, "/v1/sessions/never-used", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestSessionGetReturnsTheConversation(t *testing.T) {
	api := sessionTestServer(t)
	api.ctxCache.Set(sessionKeyPrefix+"dev-test-002", "conv-1")

	rec := httptest.NewRecorder()
	api.handleSession(rec, httptest.NewRequest(http.MethodGet, "/v1/sessions/dev-test-002", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["conversation_id"] != "conv-1" {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

// A deployment without M365 web cookies can never delete upstream, so
// local_only has to clear the mapping without reaching the network. Reaching
// it here would fail on the nil token manager.
func TestSessionDeleteLocalOnlyClearsTheMapping(t *testing.T) {
	api := sessionTestServer(t)
	api.ctxCache.Set(sessionKeyPrefix+"dev-test-002", "conv-1")

	rec := httptest.NewRecorder()
	api.handleSession(rec, httptest.NewRequest(http.MethodDelete, "/v1/sessions/dev-test-002?local_only=true", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	if got := api.ctxCache.Get(sessionKeyPrefix + "dev-test-002"); got != "" {
		t.Fatalf("the mapping survived the delete: %q", got)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	upstream, _ := body["upstream_conversation"].(map[string]any)
	if upstream == nil || upstream["deleted"] != false {
		t.Fatalf("body did not report the upstream conversation as kept: %s", rec.Body.String())
	}
}

func TestSessionDeleteReportsAMissingSession(t *testing.T) {
	api := sessionTestServer(t)
	rec := httptest.NewRecorder()
	api.handleSession(rec, httptest.NewRequest(http.MethodDelete, "/v1/sessions/never-used?local_only=true", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestSessionRejectsAnEmptyID(t *testing.T) {
	api := sessionTestServer(t)
	rec := httptest.NewRecorder()
	api.handleSession(rec, httptest.NewRequest(http.MethodGet, "/v1/sessions/", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

// Deleting upstream must not clear the mapping when it fails. Otherwise the
// only reference to a conversation that still exists is gone and the caller
// cannot retry. The cookie store is absent here, so DeleteConversation fails
// before it reaches the network.
func TestSessionDeleteKeepsTheMappingWhenUpstreamFails(t *testing.T) {
	api := sessionTestServer(t)
	api.tokenManager = auth.NewTokenManager("tenant", "client", "scope",
		filepath.Join(t.TempDir(), "rt.txt"), filepath.Join(t.TempDir(), "cache.json"))
	api.ctxCache.Set(sessionKeyPrefix+"dev-test-002", "conv-1")

	rec := httptest.NewRecorder()
	api.handleSession(rec, httptest.NewRequest(http.MethodDelete, "/v1/sessions/dev-test-002", nil))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401, body %s", rec.Code, rec.Body.String())
	}
	if got := api.ctxCache.Get(sessionKeyPrefix + "dev-test-002"); got != "conv-1" {
		t.Fatalf("the mapping was cleared despite the failed upstream delete: %q", got)
	}
}
