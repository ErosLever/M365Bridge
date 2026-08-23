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

// Every source in the chain, each one outranked by the one above it. The order
// runs from what the caller chose most deliberately to what it never chose.
func TestResolveSessionIDRanksEverySourceInOneOrder(t *testing.T) {
	// Each row removes the previous winner, so the next source must answer.
	// The rank of a source is exactly the row where it first wins.
	for _, step := range []struct {
		name   string
		src    sessionSources
		header string
		stamp  string
		want   string
	}{
		{"model suffix wins", sessionSources{"from-model", "from-previous", "from-body", "from-user"}, "from-header", "from-stamp", "from-model"},
		{"then previous_response_id", sessionSources{"", "from-previous", "from-body", "from-user"}, "from-header", "from-stamp", "from-previous"},
		{"then body session_id", sessionSources{"", "", "from-body", "from-user"}, "from-header", "from-stamp", "from-body"},
		{"then body user", sessionSources{"", "", "", "from-user"}, "from-header", "from-stamp", "from-user"},
		{"then X-Session-Id", sessionSources{}, "from-header", "from-stamp", "from-header"},
		{"then the client stamp", sessionSources{}, "", "from-stamp", "from-stamp"},
		{"nothing left", sessionSources{}, "", "", ""},
	} {
		t.Run(step.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			if step.header != "" {
				r.Header.Set("X-Session-Id", step.header)
			}
			if step.stamp != "" {
				r.Header.Set("X-Claude-Code-Session-Id", step.stamp)
			}

			if got := resolveSessionID(r, step.src); got != step.want {
				t.Errorf("session = %q, want %q", got, step.want)
			}
		})
	}
}

// The model suffix wins outright, whatever else the request carries.
func TestModelSuffixOutranksEveryOtherSource(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	r.Header.Set("X-Session-Id", "from-header")
	r.Header.Set("X-Claude-Code-Session-Id", "from-stamp")

	got := resolveSessionID(r, sessionSources{
		ModelSuffix:   "from-model",
		BodySessionID: "from-body",
		BodyUser:      "from-user",
	})

	if got != "from-model" {
		t.Errorf("session = %q, want the model suffix", got)
	}
}

// A whitespace-only value names no session, so the next source must answer.
func TestResolveSessionIDSkipsABlankSource(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	r.Header.Set("X-Session-Id", "  ")

	got := resolveSessionID(r, sessionSources{ModelSuffix: "   ", BodySessionID: " kept "})

	if got != "kept" {
		t.Errorf("session = %q, want the trimmed non-blank source", got)
	}
}

// Nothing named means nothing returned, so each caller reaches its own
// fallback rather than sharing one empty session.
func TestResolveSessionIDReportsNothingWhenTheRequestNamesNoSession(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	if got := resolveSessionID(r, sessionSources{}); got != "" {
		t.Errorf("session = %q, want empty", got)
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
