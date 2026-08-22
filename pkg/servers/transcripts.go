package servers

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/KilimcininKorOglu/M365Bridge/pkg/logging"
	"github.com/KilimcininKorOglu/M365Bridge/pkg/payload"
)

// The backend tracks conversation history by conversation ID and never sends
// it back, and ConversationClient knows no action that reads messages. So
// nothing in this process could redraw a past conversation. The browser
// interface needs exactly that, which is why the gateway keeps its own record
// of the turns it carried.
//
// This store is deliberately the only place message content reaches disk, and
// it stays switched off unless the interface is enabled.

const (
	// transcriptDir is the directory for per-session transcripts.
	transcriptDir = "data/transcripts"
	// transcriptMaxEntries caps one session's stored turns. A conversation
	// longer than this loses its oldest turns rather than growing forever.
	transcriptMaxEntries = 400
	// transcriptMaxContent caps one stored message. The interface only has to
	// redraw what was said, so a huge paste is truncated instead of stored.
	transcriptMaxContent = 128 << 10
	// transcriptMaxSessions caps how many session files the store keeps. The
	// oldest files go first, measured by modification time.
	transcriptMaxSessions = 512
)

// TranscriptEntry is one stored message in a session.
type TranscriptEntry struct {
	Role      string `json:"role"`
	Content   string `json:"content"`
	Model     string `json:"model,omitempty"`
	Thinking  string `json:"thinking,omitempty"`
	CreatedAt int64  `json:"created_at"`
}

// TranscriptStore keeps one JSON file of turns per session id.
type TranscriptStore struct {
	dir        string
	mu         sync.Mutex
	writeFile  func(string, []byte, os.FileMode) error
	removeFile func(string) error
}

// NewTranscriptStore creates a store rooted at dir.
func NewTranscriptStore(dir string) *TranscriptStore {
	if err := os.MkdirAll(dir, 0700); err != nil {
		logging.Errorf("transcripts: cannot create %s: %v", dir, err)
	}
	return &TranscriptStore{
		dir:        dir,
		writeFile:  os.WriteFile,
		removeFile: os.Remove,
	}
}

// path returns the file for a session id.
//
// The id arrives in a request header, so it never becomes a path segment; the
// file name is a hash of it instead.
func (ts *TranscriptStore) path(sessionID string) string {
	sum := sha256.Sum256([]byte(sessionID))
	return filepath.Join(ts.dir, hex.EncodeToString(sum[:])+".json")
}

// Load returns the stored turns for a session, oldest first.
func (ts *TranscriptStore) Load(sessionID string) []TranscriptEntry {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return ts.read(sessionID)
}

// read loads a transcript without taking the lock. Callers hold it already.
func (ts *TranscriptStore) read(sessionID string) []TranscriptEntry {
	data, err := os.ReadFile(ts.path(sessionID))
	if err != nil {
		return nil
	}
	var entries []TranscriptEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		logging.Errorf("transcripts: cannot decode the record for a session: %v", err)
		return nil
	}
	return entries
}

// Append adds one turn to a session and writes the file.
func (ts *TranscriptStore) Append(sessionID string, entry TranscriptEntry) {
	if sessionID == "" || entry.Content == "" {
		return
	}
	if entry.CreatedAt == 0 {
		entry.CreatedAt = time.Now().Unix()
	}
	entry.Content = truncateRunes(entry.Content, transcriptMaxContent)
	entry.Thinking = truncateRunes(entry.Thinking, transcriptMaxContent)

	ts.mu.Lock()
	defer ts.mu.Unlock()

	entries := append(ts.read(sessionID), entry)
	if len(entries) > transcriptMaxEntries {
		entries = entries[len(entries)-transcriptMaxEntries:]
	}
	data, err := json.Marshal(entries)
	if err != nil {
		logging.Errorf("transcripts: cannot encode the record for a session: %v", err)
		return
	}
	if err := ts.writeFile(ts.path(sessionID), data, 0600); err != nil {
		logging.Errorf("transcripts: cannot write the record for a session: %v", err)
		return
	}
	ts.evict()
}

// Replace overwrites a session's stored turns.
//
// Append cannot do this job: importing a conversation held elsewhere produces
// the whole history at once, and adding it to whatever the session already
// holds would show the imported turns twice.
func (ts *TranscriptStore) Replace(sessionID string, entries []TranscriptEntry) {
	if sessionID == "" {
		return
	}
	now := time.Now().Unix()
	cleaned := make([]TranscriptEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.Content == "" {
			continue
		}
		if entry.CreatedAt == 0 {
			entry.CreatedAt = now
		}
		entry.Content = truncateRunes(entry.Content, transcriptMaxContent)
		entry.Thinking = truncateRunes(entry.Thinking, transcriptMaxContent)
		cleaned = append(cleaned, entry)
	}
	if len(cleaned) > transcriptMaxEntries {
		cleaned = cleaned[len(cleaned)-transcriptMaxEntries:]
	}

	ts.mu.Lock()
	defer ts.mu.Unlock()

	data, err := json.Marshal(cleaned)
	if err != nil {
		logging.Errorf("transcripts: cannot encode the record for a session: %v", err)
		return
	}
	if err := ts.writeFile(ts.path(sessionID), data, 0600); err != nil {
		logging.Errorf("transcripts: cannot write the record for a session: %v", err)
		return
	}
	ts.evict()
}

// Delete removes a session's transcript. A missing file is not an error,
// because a session may never have recorded a turn.
func (ts *TranscriptStore) Delete(sessionID string) {
	if sessionID == "" {
		return
	}
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if err := ts.removeFile(ts.path(sessionID)); err != nil && !os.IsNotExist(err) {
		logging.Errorf("transcripts: cannot delete the record for a session: %v", err)
	}
}

// evict drops the oldest files once the store holds too many. Callers hold the
// lock. A session deleted through the API takes its file with it, but a
// session that is simply abandoned leaves one behind, so the store needs a
// ceiling of its own.
func (ts *TranscriptStore) evict() {
	names, err := os.ReadDir(ts.dir)
	if err != nil || len(names) <= transcriptMaxSessions {
		return
	}
	type aged struct {
		path    string
		modTime time.Time
	}
	files := make([]aged, 0, len(names))
	for _, name := range names {
		info, err := name.Info()
		if err != nil {
			continue
		}
		files = append(files, aged{filepath.Join(ts.dir, name.Name()), info.ModTime()})
	}
	slices.SortFunc(files, func(a, b aged) int { return a.modTime.Compare(b.modTime) })
	for i := 0; i < len(files)-transcriptMaxSessions; i++ {
		if err := ts.removeFile(files[i].path); err != nil {
			logging.Errorf("transcripts: cannot evict an old record: %v", err)
		}
	}
}

// truncateRunes cuts a string to at most limit bytes without splitting a rune.
func truncateRunes(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

// lastUserMessage returns the text of the turn the caller is asking about.
//
// A request carries the whole conversation the client holds, but only the last
// user message is new; recording the rest would repeat every earlier turn on
// every request. A progress note is skipped because it reports a running tool
// rather than starting a turn.
func lastUserMessage(messages []payload.Message) string {
	for _, msg := range slices.Backward(messages) {
		if msg.Role != "user" || msg.ToolProgress {
			continue
		}
		if strings.TrimSpace(msg.Content) == "" {
			continue
		}
		return msg.Content
	}
	return ""
}

// recordUserTurn stores the message the caller just sent.
func (api *APIServer) recordUserTurn(sid string, messages []payload.Message) {
	if api.transcripts == nil || sid == "" {
		return
	}
	api.transcripts.Append(sid, TranscriptEntry{Role: "user", Content: lastUserMessage(messages)})
}

// recordAssistantTurn stores the answer the backend produced.
func (api *APIServer) recordAssistantTurn(sid, model, content, thinking string) {
	if api.transcripts == nil || sid == "" {
		return
	}
	api.transcripts.Append(sid, TranscriptEntry{
		Role:     "assistant",
		Content:  content,
		Model:    model,
		Thinking: thinking,
	})
}

// dropTranscript clears a session's record. A turn that produced nothing also
// drops the conversation mapping, so leaving the record behind would show the
// old history under a session that now starts a new conversation.
func (api *APIServer) dropTranscript(sid string) {
	if api.transcripts == nil {
		return
	}
	api.transcripts.Delete(sid)
}
