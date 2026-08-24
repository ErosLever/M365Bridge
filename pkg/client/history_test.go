package client

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// buildPage wraps loader data the way the rendered conversation page does: the
// JSON is a JavaScript string literal handed to JSON.parse, so it is encoded
// twice.
func buildPage(t *testing.T, loaderData any) string {
	t.Helper()
	inner, err := json.Marshal(map[string]any{"loaderData": loaderData})
	if err != nil {
		t.Fatalf("fixture did not encode: %v", err)
	}
	literal, err := json.Marshal(string(inner))
	if err != nil {
		t.Fatalf("fixture literal did not encode: %v", err)
	}
	return `<html><body><div>markup</div><script nonce="x">` +
		hydrationMarker + string(literal) + `);</script></body></html>`
}

// route wraps messages the way one loader entry carries them.
func route(messages ...map[string]any) map[string]any {
	return map[string]any{
		"store": map[string]any{
			"rawConversationResponse": map[string]any{"messages": messages},
		},
	}
}

func userTurn(text string) map[string]any {
	return map[string]any{"text": text, "author": "user", "messageId": "u1", "createdAt": "2026-08-20T10:09:04Z"}
}

func botTurn(text string) map[string]any {
	return map[string]any{"text": text, "author": "bot", "messageId": "b1", "createdAt": "2026-08-20T10:09:06Z"}
}

func TestParseConversationPageReadsBothRolesInOrder(t *testing.T) {
	page := buildPage(t, map[string]any{
		"root": map[string]any{"store": map[string]any{"coreAppsContent": []any{}}},
		"chat-history": route(
			userTurn("Bugün Ankara'da hava nasıl?"),
			botTurn("Ankara'da hava güneşli."),
			userTurn("Yarın?"),
			botTurn("Yarın yağmurlu."),
		),
	})

	got, err := parseConversationPage(page)
	if err != nil {
		t.Fatalf("parseConversationPage: %v", err)
	}
	want := []HistoryMessage{
		{Role: "user", Text: "Bugün Ankara'da hava nasıl?"},
		{Role: "assistant", Text: "Ankara'da hava güneşli."},
		{Role: "user", Text: "Yarın?"},
		{Role: "assistant", Text: "Yarın yağmurlu."},
	}
	if len(got) != len(want) {
		t.Fatalf("read %d turns, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i].Role != want[i].Role || got[i].Text != want[i].Text {
			t.Errorf("turn %d = %q/%q, want %q/%q", i, got[i].Role, got[i].Text, want[i].Role, want[i].Text)
		}
	}
}

// M365 interleaves its own progress notes and tool traffic into the same
// messages array. This is the rule carriesAnswerText already applies to the
// live stream, and letting it slip here would put backend internals into a
// conversation as if the assistant had said them.
func TestParseConversationPageSkipsProgressAndBackendTools(t *testing.T) {
	page := buildPage(t, map[string]any{
		"chat-history": route(
			userTurn("kod yaz"),
			map[string]any{"text": "Queuing things up…", "author": "bot", "messageType": "Progress", "contentType": "EarlyProgress"},
			map[string]any{"text": "print(1)", "author": "bot", "messageType": "GeneratedCode"},
			map[string]any{"text": "hava durumu", "author": "bot", "messageType": "InternalSearchQuery"},
			botTurn("İşte kod."),
		),
	})

	got, err := parseConversationPage(page)
	if err != nil {
		t.Fatalf("parseConversationPage: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("read %d turns, want 2: %+v", len(got), got)
	}
	for _, msg := range got {
		for _, leaked := range []string{"Queuing", "print(1)", "hava durumu"} {
			if strings.Contains(msg.Text, leaked) {
				t.Errorf("backend traffic reached the caller: %q", msg.Text)
			}
		}
	}
}

// A stored answer carries the same Private Use Area citation markers the live
// stream does, and they are not answer text in either place.
func TestParseConversationPageStripsCitationMarkers(t *testing.T) {
	marked := "Hava güneşli. " + string(citationStart) + "citeturn1search1" + string(citationEnd)
	page := buildPage(t, map[string]any{"chat-history": route(botTurn(marked))})

	got, err := parseConversationPage(page)
	if err != nil {
		t.Fatalf("parseConversationPage: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("read %d turns: %+v", len(got), got)
	}
	if got[0].Text != "Hava güneşli." {
		t.Errorf("text = %q, want %q", got[0].Text, "Hava güneşli.")
	}
}

// The route id that owns the conversation is not fixed, so the reader must find
// the entry rather than assume its name.
func TestParseConversationPageFindsTheRouteUnderAnyName(t *testing.T) {
	page := buildPage(t, map[string]any{
		"root":              map[string]any{"store": map[string]any{"navPane": []any{1, 2, 3}}},
		"some-other-route":  map[string]any{"store": map[string]any{}},
		"a-renamed-history": route(userTurn("Merhaba")),
	})

	got, err := parseConversationPage(page)
	if err != nil {
		t.Fatalf("parseConversationPage: %v", err)
	}
	if len(got) != 1 || got[0].Text != "Merhaba" {
		t.Errorf("read %+v", got)
	}
}

// A loader entry that is not an object must not stop the search, because the
// router puts whatever a route returned in there.
func TestParseConversationPageSurvivesAnUnexpectedLoaderEntry(t *testing.T) {
	page := buildPage(t, map[string]any{
		"nulled":       nil,
		"a-list":       []any{1, 2},
		"a-string":     "text",
		"chat-history": route(botTurn("Cevap.")),
	})

	got, err := parseConversationPage(page)
	if err != nil {
		t.Fatalf("parseConversationPage: %v", err)
	}
	if len(got) != 1 || got[0].Text != "Cevap." {
		t.Errorf("read %+v", got)
	}
}

func TestParseConversationPageKeepsTheMessageID(t *testing.T) {
	page := buildPage(t, map[string]any{"chat-history": route(botTurn("Cevap."))})
	got, err := parseConversationPage(page)
	if err != nil {
		t.Fatalf("parseConversationPage: %v", err)
	}
	if got[0].MessageID != "b1" {
		t.Errorf("message id = %q, want b1", got[0].MessageID)
	}
	if got[0].CreatedAt != "2026-08-20T10:09:06Z" {
		t.Errorf("created at = %q", got[0].CreatedAt)
	}
}

// A page this reader cannot decode must not look like an empty conversation,
// because a caller cannot tell the two apart.
func TestParseConversationPageReportsWhatItCannotRead(t *testing.T) {
	cases := map[string]string{
		"no hydration data":  `<html><body><div id="root"></div></body></html>`,
		"not a string":       `<script>` + hydrationMarker + `{"loaderData":{}});</script>`,
		"unterminated":       `<script>` + hydrationMarker + `"{\"loaderData\":{}}`,
		"no conversation":    buildPage(t, map[string]any{"root": map[string]any{"store": map[string]any{}}}),
		"an empty turn list": buildPage(t, map[string]any{"chat-history": route()}),
	}
	for name, page := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := parseConversationPage(page)
			if err == nil {
				t.Fatalf("no error, read %+v", got)
			}
			if !errors.Is(err, ErrHistoryUnavailable) {
				t.Errorf("error = %v, want one wrapping ErrHistoryUnavailable", err)
			}
		})
	}
}

// The payload holds escaped quotes throughout, so a scan that stops at the
// first quote would truncate every real page.
func TestExtractHydrationDataStopsAtTheRealEnd(t *testing.T) {
	page := buildPage(t, map[string]any{"chat-history": route(botTurn(`he said "yes" and left`))})
	blob, err := extractHydrationData(page)
	if err != nil {
		t.Fatalf("extractHydrationData: %v", err)
	}
	var probe struct {
		LoaderData map[string]json.RawMessage `json:"loaderData"`
	}
	if err := json.Unmarshal([]byte(blob), &probe); err != nil {
		t.Fatalf("the extracted blob is not whole: %v", err)
	}
	if _, ok := probe.LoaderData["chat-history"]; !ok {
		t.Error("the extracted blob lost its route entry")
	}
}

func TestValidateConversationIDRejectsPathEscapes(t *testing.T) {
	for _, id := range []string{
		"",
		"../../etc/passwd",
		"abc/def",
		"abc?x=1",
		"abc#frag",
		"abc def",
		strings.Repeat("a", conversationIDMax+1),
	} {
		if err := validateConversationID(id); err == nil {
			t.Errorf("validateConversationID(%q) accepted it", id)
		}
	}
}

func TestValidateConversationIDAcceptsARealID(t *testing.T) {
	for _, id := range []string{
		"da221759-b32a-4a5b-94d4-f6048387744d",
		"a1b2_c3d4",
	} {
		if err := validateConversationID(id); err != nil {
			t.Errorf("validateConversationID(%q) = %v", id, err)
		}
	}
}
