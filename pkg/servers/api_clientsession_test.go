package servers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Claude Code and Codex each stamp their own session on every request, under a
// name neither can be told to change. Without those names both clients fell
// through to the message hash, which collides whenever two conversations open
// with the same first message.
func TestClientStampedSessionIDReadsEachClientsHeader(t *testing.T) {
	for name, c := range map[string]struct {
		header string
		value  string
		want   string
	}{
		"claude code":      {"X-Claude-Code-Session-Id", "cc-1", "cc-1"},
		"codex":            {"Session-Id", "codex-1", "codex-1"},
		"codex lowercase":  {"session-id", "codex-2", "codex-2"},
		"blank value":      {"X-Claude-Code-Session-Id", "", ""},
		"whitespace value": {"Session-Id", "   ", ""},
		"unrelated header": {"X-Request-Id", "req-1", ""},
	} {
		t.Run(name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
			r.Header.Set(c.header, c.value)

			if got := clientStampedSessionID(r); got != c.want {
				t.Errorf("clientStampedSessionID = %q, want %q", got, c.want)
			}
		})
	}
}

// A request that carries none of them must report nothing, so the caller falls
// through to the hash rather than keying every client on one empty session.
func TestClientStampedSessionIDReportsNothingWithoutAHeader(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	if got := clientStampedSessionID(r); got != "" {
		t.Errorf("clientStampedSessionID = %q, want empty", got)
	}
}

// Codex sends thread-id carrying the same value as session-id, so reading it
// would only ever answer for a request that already carries session-id. Codex's
// x-codex-turn-metadata is stable across every session on one machine, so
// keying a conversation on it would merge unrelated sessions.
func TestClientStampedSessionIDIgnoresTheRedundantAndTheMachineStableHeaders(t *testing.T) {
	for _, name := range []string{"Thread-Id", "X-Codex-Turn-Metadata"} {
		r := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
		r.Header.Set(name, "should-not-be-read")

		if got := clientStampedSessionID(r); got != "" {
			t.Errorf("%s produced session %q, want empty", name, got)
		}
	}
}

// The client writes its header without being asked, so anything the caller set
// deliberately outranks it.
func TestDeliberateSessionOutranksTheClientStamp(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	r.Header.Set("X-Claude-Code-Session-Id", "stamped")
	r.Header.Set("X-Session-Id", "chosen")

	api := &APIServer{}
	if got := api.getSessionID(r, nil); got != "chosen" {
		t.Errorf("session = %q, want the deliberately set X-Session-Id", got)
	}
}

func TestBodyFieldsOutrankTheClientStamp(t *testing.T) {
	for field, want := range map[string]string{"session_id": "from-body", "user": "from-user"} {
		r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		r.Header.Set("X-Claude-Code-Session-Id", "stamped")

		api := &APIServer{}
		if got := api.getSessionID(r, map[string]any{field: want}); got != want {
			t.Errorf("with %s set, session = %q, want %q", field, got, want)
		}
	}
}

// The stamp is what the change buys: it must win over the hash, which is the
// only thing that answered for these clients before.
func TestTheClientStampOutranksTheMessageHash(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	r.Header.Set("X-Claude-Code-Session-Id", "stamped")

	api := &APIServer{}
	got := api.getSessionID(r, map[string]any{
		"messages": []any{map[string]any{"role": "user", "content": "hello"}},
	})

	if got != "stamped" {
		t.Errorf("session = %q, want the client stamp rather than a hash", got)
	}
}

// The preflight must name every header the routes read, or a browser client
// cannot send them.
func TestCORSAllowsEveryHeaderTheRoutesRead(t *testing.T) {
	w := httptest.NewRecorder()
	api := &APIServer{}
	api.handleCORS(w, httptest.NewRequest(http.MethodOptions, "/v1/messages", nil))

	allowed := w.Header().Get("Access-Control-Allow-Headers")
	for _, name := range append([]string{"X-Session-Id"}, clientSessionHeaders...) {
		if !containsHeaderName(allowed, name) {
			t.Errorf("Access-Control-Allow-Headers %q does not name %q", allowed, name)
		}
	}
}

func containsHeaderName(list, name string) bool {
	for part := range strings.SplitSeq(list, ",") {
		if http.CanonicalHeaderKey(strings.TrimSpace(part)) == http.CanonicalHeaderKey(name) {
			return true
		}
	}
	return false
}
