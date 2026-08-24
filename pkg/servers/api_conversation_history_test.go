package servers

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

// The conversation route now carries a subresource, so the path split decides
// what runs. Anything it does not serve has to stop before the handler reaches
// upstream, because the only other outcome is a request M365 never asked for.
func TestConversationSubresourceRoutingRejectsWhatItDoesNotServe(t *testing.T) {
	api := &APIServer{}

	cases := []struct {
		name   string
		method string
		path   string
	}{
		{"an unknown subresource", http.MethodGet, "/v1/conversations/abc/unknown"},
		{"a nested unknown subresource", http.MethodGet, "/v1/conversations/abc/messages/1"},
		{"a write to the history", http.MethodPost, "/v1/conversations/abc/messages"},
		{"a delete of the history", http.MethodDelete, "/v1/conversations/abc/messages"},
		{"no conversation id", http.MethodGet, "/v1/conversations//messages"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			// A handler that reached upstream would panic on the nil token
			// manager, which is what makes this test meaningful: it passes only
			// because the request was refused first.
			api.handleConversation(recorder, httptest.NewRequest(tc.method, tc.path, nil))

			if recorder.Code != http.StatusNotFound {
				t.Errorf("status = %d, want 404", recorder.Code)
			}
		})
	}
}

// An imported conversation arrives whole, so it replaces the record instead of
// extending it. Appending would show every turn twice on a second import.
func TestTranscriptReplaceOverwritesInsteadOfAppending(t *testing.T) {
	store := NewTranscriptStore(t.TempDir())
	store.Append("s1", TranscriptEntry{Role: "user", Content: "eski"})

	imported := []TranscriptEntry{
		{Role: "user", Content: "soru", CreatedAt: 100},
		{Role: "assistant", Content: "cevap", CreatedAt: 200},
	}
	store.Replace("s1", imported)
	store.Replace("s1", imported)

	got := store.Load("s1")
	if len(got) != 2 {
		t.Fatalf("stored %d turns, want 2: %+v", len(got), got)
	}
	if got[0].Content != "soru" || got[1].Content != "cevap" {
		t.Errorf("stored %+v", got)
	}
	if got[0].CreatedAt != 100 {
		t.Errorf("Replace overwrote a supplied timestamp: %d", got[0].CreatedAt)
	}
}

// A turn with no text is not a turn. It would render as a blank bubble.
func TestTranscriptReplaceDropsEmptyEntries(t *testing.T) {
	store := NewTranscriptStore(t.TempDir())
	store.Replace("s1", []TranscriptEntry{
		{Role: "user", Content: "soru"},
		{Role: "assistant", Content: ""},
	})
	if got := store.Load("s1"); len(got) != 1 {
		t.Errorf("stored %d turns, want 1: %+v", len(got), got)
	}
}

func TestTranscriptReplaceCapsTheStoredTurns(t *testing.T) {
	store := NewTranscriptStore(t.TempDir())
	entries := make([]TranscriptEntry, transcriptMaxEntries+20)
	for i := range entries {
		entries[i] = TranscriptEntry{Role: "user", Content: string(rune('a' + i%26))}
	}
	store.Replace("s1", entries)

	if got := store.Load("s1"); len(got) != transcriptMaxEntries {
		t.Errorf("stored %d turns, want %d", len(got), transcriptMaxEntries)
	}
}

// The store writes message content, so its file must not be readable by other
// users on the host.
func TestTranscriptReplaceWritesAPrivateFile(t *testing.T) {
	dir := t.TempDir()
	store := NewTranscriptStore(dir)
	store.Replace("s1", []TranscriptEntry{{Role: "user", Content: "gizli"}})

	info, err := os.Stat(store.path("s1"))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("file mode = %o, want 600", perm)
	}
}
