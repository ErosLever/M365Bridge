package servers

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/KilimcininKorOglu/M365Bridge/pkg/payload"
	"github.com/KilimcininKorOglu/M365Bridge/pkg/toolcalling"
)

// usageProbeMessages is the turn every usage test counts, so a divergence
// between two endpoints shows up as a difference in the reported number rather
// than a difference in the input.
func usageProbeMessages() []payload.Message {
	return []payload.Message{
		{Role: "system", Content: "You are terse."},
		{Role: "user", Content: "Reply with exactly: OK"},
	}
}

// The Go representation of a message slice prints every field of every
// message, empty ones included, wrapped in braces and brackets. Running the
// encoder over that counts punctuation the request never carried and skips the
// protocol framing the request did carry, so it answers a different question
// from countPromptTokens. This test guards the difference.
func TestStructPrintIsNotAPromptCount(t *testing.T) {
	messages := usageProbeMessages()

	correct := countPromptTokens(messages, nil, "")
	structPrint := countTokens(fmt.Sprint(messages))
	if correct == structPrint {
		t.Fatalf("both counters returned %d, so the test cannot tell them apart", correct)
	}

	// The empty fields are what makes the printed form diverge from the text
	// that was actually sent.
	printed := fmt.Sprint(messages)
	if !strings.Contains(printed, "[] []") {
		t.Fatalf("fmt.Sprint no longer prints the empty fields; this guard is stale: %q", printed)
	}
}

// The Responses policy defaults its prompt choice to "auto" for every request,
// including one that declares no tools. Billing that framing would make the
// same turn cost more on /v1/responses than on /v1/chat/completions.
func TestToolChoiceFramingNeedsDeclaredTools(t *testing.T) {
	messages := usageProbeMessages()

	bare := countPromptTokens(messages, nil, "")
	withDefaultChoice := countPromptTokens(messages, nil, "auto")
	if bare != withDefaultChoice {
		t.Fatalf("a tool choice without tools changed the count from %d to %d", bare, withDefaultChoice)
	}

	tools := []toolcalling.ToolDef{{
		Type:     "function",
		Function: toolcalling.ToolDefFunc{Name: "get_weather"},
	}}
	withTools := countPromptTokens(messages, tools, "")
	withToolsAndChoice := countPromptTokens(messages, tools, "auto")
	if withToolsAndChoice != withTools+toolChoiceProtocolTokens {
		t.Fatalf("a real tool choice added %d tokens, want %d",
			withToolsAndChoice-withTools, toolChoiceProtocolTokens)
	}
}

// decodeUsage reads the usage object out of a JSON body under the given key.
func decodeUsage(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var decoded map[string]any
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decode body %s: %v", body, err)
	}
	usage, ok := decoded["usage"].(map[string]any)
	if !ok {
		t.Fatalf("body carries no usage object: %s", body)
	}
	return usage
}

// usageNumber reads one field as an int, failing when it is absent.
func usageNumber(t *testing.T, usage map[string]any, key string) int {
	t.Helper()
	value, ok := usage[key].(float64)
	if !ok {
		t.Fatalf("usage has no numeric %s: %#v", key, usage)
	}
	return int(value)
}

// requireUsageSource fails when the usage object does not name its encoder.
func requireUsageSource(t *testing.T, usage map[string]any) {
	t.Helper()
	source, ok := usage["usage_source"].(string)
	if !ok || source == "" {
		t.Fatalf("usage does not name its source: %#v", usage)
	}
}
