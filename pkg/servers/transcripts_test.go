package servers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/KilimcininKorOglu/M365Bridge/pkg/models"
	"github.com/KilimcininKorOglu/M365Bridge/pkg/payload"
)

func TestTranscriptStoreRoundTripsATurn(t *testing.T) {
	store := NewTranscriptStore(t.TempDir())
	store.Append("dev-test-002", TranscriptEntry{Role: "user", Content: "merhaba"})
	store.Append("dev-test-002", TranscriptEntry{Role: "assistant", Content: "selam", Model: "gpt-5.5"})

	entries := store.Load("dev-test-002")
	if len(entries) != 2 {
		t.Fatalf("loaded %d entries, want 2", len(entries))
	}
	if entries[0].Role != "user" || entries[0].Content != "merhaba" {
		t.Errorf("first entry = %#v", entries[0])
	}
	if entries[1].Model != "gpt-5.5" {
		t.Errorf("second entry lost its model: %#v", entries[1])
	}
	for i, entry := range entries {
		if entry.CreatedAt == 0 {
			t.Errorf("entry %d carries no timestamp", i)
		}
	}
}

// A session id arrives in a request header, so it must never reach the
// filesystem as a path segment.
func TestTranscriptStoreKeepsAHostileSessionIDOutOfThePath(t *testing.T) {
	dir := t.TempDir()
	store := NewTranscriptStore(dir)
	store.Append("../../escaped", TranscriptEntry{Role: "user", Content: "x"})

	names, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(names) != 1 {
		t.Fatalf("wrote %d files, want 1", len(names))
	}
	if strings.Contains(names[0].Name(), "..") || strings.Contains(names[0].Name(), "escaped") {
		t.Fatalf("the session id reached the file name: %q", names[0].Name())
	}
	if got := store.Load("../../escaped"); len(got) != 1 {
		t.Fatalf("the entry did not round-trip: %#v", got)
	}
}

func TestTranscriptStoreDropsTheOldestTurnsAtTheCap(t *testing.T) {
	store := NewTranscriptStore(t.TempDir())
	for i := range transcriptMaxEntries + 10 {
		store.Append("dev-test-002", TranscriptEntry{Role: "user", Content: string(rune('a' + i%26)), CreatedAt: int64(i + 1)})
	}

	entries := store.Load("dev-test-002")
	if len(entries) != transcriptMaxEntries {
		t.Fatalf("kept %d entries, want the cap of %d", len(entries), transcriptMaxEntries)
	}
	// The eleventh append is the first survivor once ten were dropped.
	if entries[0].CreatedAt != 11 {
		t.Fatalf("oldest surviving entry = %d, want 11", entries[0].CreatedAt)
	}
}

func TestTranscriptStoreTruncatesAHugeMessage(t *testing.T) {
	store := NewTranscriptStore(t.TempDir())
	store.Append("dev-test-002", TranscriptEntry{Role: "user", Content: strings.Repeat("ç", transcriptMaxContent)})

	entries := store.Load("dev-test-002")
	if len(entries) != 1 {
		t.Fatalf("loaded %d entries, want 1", len(entries))
	}
	if len(entries[0].Content) > transcriptMaxContent {
		t.Fatalf("stored %d bytes, over the cap of %d", len(entries[0].Content), transcriptMaxContent)
	}
	// A cut in the middle of a multi-byte rune would leave invalid UTF-8, and
	// the JSON encoder would replace it rather than fail, so the corruption
	// would only show in the interface.
	if !json.Valid([]byte(`"` + entries[0].Content + `"`)) {
		t.Fatal("truncation split a rune and produced invalid UTF-8")
	}
}

func TestTranscriptStoreDeleteRemovesTheRecord(t *testing.T) {
	store := NewTranscriptStore(t.TempDir())
	store.Append("dev-test-002", TranscriptEntry{Role: "user", Content: "merhaba"})
	store.Delete("dev-test-002")

	if got := store.Load("dev-test-002"); len(got) != 0 {
		t.Fatalf("the record survived the delete: %#v", got)
	}
	// Deleting a session that never recorded a turn is normal, not an error.
	store.Delete("never-used")
}

func TestLastUserMessagePicksTheNewTurn(t *testing.T) {
	messages := []payload.Message{
		{Role: "user", Content: "eski soru"},
		{Role: "assistant", Content: "eski cevap"},
		{Role: "user", Content: "yeni soru"},
		{Role: "user", Content: "tool still running", ToolProgress: true},
	}
	if got := lastUserMessage(messages); got != "yeni soru" {
		t.Fatalf("lastUserMessage = %q, want \"yeni soru\"", got)
	}
	if got := lastUserMessage(nil); got != "" {
		t.Fatalf("lastUserMessage(nil) = %q, want empty", got)
	}
}

// A gateway that only proxies must not write message content to disk, so the
// recording has to stay off with the interface.
func TestNoTranscriptIsRecordedWhenTheInterfaceIsOff(t *testing.T) {
	api := NewAPIServer(&models.Config{EnableWebUI: false}, nil)
	if api.transcripts != nil {
		t.Fatal("a store was created while the interface is disabled")
	}
	// The helpers must tolerate the nil store rather than panic.
	api.recordUserTurn("dev-test-002", []payload.Message{{Role: "user", Content: "merhaba"}})
	api.recordAssistantTurn("dev-test-002", "gpt-5.5", "selam", "")
	api.dropTranscript("dev-test-002")
}

// transcriptTestServer builds a server whose stores live in temp dirs.
func transcriptTestServer(t *testing.T) *APIServer {
	t.Helper()
	return &APIServer{
		config:      &models.Config{EnableWebUI: true},
		ctxCache:    NewContextCache(t.TempDir()),
		transcripts: NewTranscriptStore(t.TempDir()),
	}
}

func TestSessionMessagesReturnsTheRecordedTurns(t *testing.T) {
	api := transcriptTestServer(t)
	api.recordUserTurn("dev-test-002", []payload.Message{{Role: "user", Content: "merhaba"}})
	api.recordAssistantTurn("dev-test-002", "gpt-5.5", "selam", "düşünce")

	rec := httptest.NewRecorder()
	api.handleSession(rec, httptest.NewRequest(http.MethodGet, "/v1/sessions/dev-test-002/messages", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Object    string `json:"object"`
		SessionID string `json:"session_id"`
		Data      []struct {
			Role     string `json:"role"`
			Content  string `json:"content"`
			Model    string `json:"model"`
			Thinking string `json:"thinking"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode %s: %v", rec.Body.Bytes(), err)
	}
	if body.Object != "list" || body.SessionID != "dev-test-002" || len(body.Data) != 2 {
		t.Fatalf("body = %s", rec.Body.String())
	}
	if body.Data[0].Role != "user" || body.Data[0].Content != "merhaba" {
		t.Errorf("first entry = %#v", body.Data[0])
	}
	if body.Data[1].Model != "gpt-5.5" || body.Data[1].Thinking != "düşünce" {
		t.Errorf("second entry = %#v", body.Data[1])
	}
}

// A conversation started outside this gateway has no record, and an empty list
// is the honest answer rather than a 404 that reads as "no such session".
func TestSessionMessagesReturnsAnEmptyListForAnUnknownSession(t *testing.T) {
	api := transcriptTestServer(t)
	rec := httptest.NewRecorder()
	api.handleSession(rec, httptest.NewRequest(http.MethodGet, "/v1/sessions/never-used/messages", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var body struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Data) != 0 {
		t.Fatalf("data = %v, want an empty list", body.Data)
	}
	if !strings.Contains(rec.Body.String(), `"data":[]`) {
		t.Fatalf("data serialized as null instead of an empty array: %s", rec.Body.String())
	}
}

func TestSessionMessagesReportsThatRecordingIsOff(t *testing.T) {
	api := &APIServer{config: &models.Config{}, ctxCache: NewContextCache(t.TempDir())}
	rec := httptest.NewRecorder()
	api.handleSession(rec, httptest.NewRequest(http.MethodGet, "/v1/sessions/dev-test-002/messages", nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "transcripts_disabled") {
		t.Fatalf("body does not name the cause: %s", rec.Body.String())
	}
}

func TestSessionMessagesRejectsOtherMethodsAndPaths(t *testing.T) {
	api := transcriptTestServer(t)

	rec := httptest.NewRecorder()
	api.handleSession(rec, httptest.NewRequest(http.MethodDelete, "/v1/sessions/dev-test-002/messages", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("DELETE status = %d, want 405", rec.Code)
	}

	rec = httptest.NewRecorder()
	api.handleSession(rec, httptest.NewRequest(http.MethodGet, "/v1/sessions/dev-test-002/anything", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("unknown sub-path status = %d, want 404", rec.Code)
	}
}

// Deleting a session must take its transcript with it. Otherwise the next turn
// under the same id starts a new conversation while the interface still shows
// the old history.
func TestSessionDeleteAlsoClearsTheTranscript(t *testing.T) {
	api := transcriptTestServer(t)
	api.ctxCache.Set(sessionKeyPrefix+"dev-test-002", "conv-1")
	api.recordUserTurn("dev-test-002", []payload.Message{{Role: "user", Content: "merhaba"}})

	rec := httptest.NewRecorder()
	api.handleSession(rec, httptest.NewRequest(http.MethodDelete, "/v1/sessions/dev-test-002?local_only=true", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}

	if got := api.transcripts.Load("dev-test-002"); len(got) != 0 {
		t.Fatalf("the transcript survived the session delete: %#v", got)
	}
}

// An empty turn drops the conversation mapping, so the stored turns now belong
// to a conversation this id no longer points at.
func TestAnEmptyTurnClearsTheTranscriptWithTheMapping(t *testing.T) {
	api := transcriptTestServer(t)
	api.ctxCache.Set(sessionKeyPrefix+"dev-test-002", "conv-old")
	api.recordUserTurn("dev-test-002", []payload.Message{{Role: "user", Content: "merhaba"}})

	api.updateChatStreamSession("dev-test-002", "gpt-5.5", "conv-new", "", "", nil)

	if got := api.ctxCache.Get(sessionKeyPrefix + "dev-test-002"); got != "" {
		t.Fatalf("the mapping survived an empty turn: %q", got)
	}
	if got := api.transcripts.Load("dev-test-002"); len(got) != 0 {
		t.Fatalf("the transcript survived an empty turn: %#v", got)
	}
}

func TestAProductiveTurnRecordsTheAnswer(t *testing.T) {
	api := transcriptTestServer(t)
	api.updateChatStreamSession("dev-test-002", "gpt-5.5", "conv-new", "selam", "düşünce", nil)

	entries := api.transcripts.Load("dev-test-002")
	if len(entries) != 1 {
		t.Fatalf("recorded %d entries, want 1", len(entries))
	}
	if entries[0].Role != "assistant" || entries[0].Content != "selam" || entries[0].Model != "gpt-5.5" {
		t.Fatalf("entry = %#v", entries[0])
	}
	if entries[0].Thinking != "düşünce" {
		t.Fatalf("thinking was not recorded: %#v", entries[0])
	}
}
