package servers

import (
	"strings"
	"testing"
)

// Codex appends the trigger as the last input item of an ordinary
// /v1/responses request rather than calling /v1/responses/compact, so the
// detector has to find it there.
func TestResponsesInputHasCompactionTriggerFindsItLast(t *testing.T) {
	input := []any{
		map[string]any{"role": "user", "content": "Fix the auth bug"},
		map[string]any{"role": "assistant", "content": "Added the missing parameter."},
		map[string]any{"type": "compaction_trigger"},
	}
	if !responsesInputHasCompactionTrigger(input) {
		t.Fatal("a trailing compaction_trigger was not detected")
	}
}

func TestResponsesInputHasCompactionTriggerRejectsOrdinaryInput(t *testing.T) {
	cases := map[string]any{
		"nil":            nil,
		"string input":   "Summarize this",
		"empty array":    []any{},
		"message only":   []any{map[string]any{"role": "user", "content": "hello"}},
		"non-map item":   []any{"compaction_trigger"},
		"other item":     []any{map[string]any{"type": "function_call_output", "call_id": "call_1"}},
		"summary item":   []any{map[string]any{"type": "compaction", "encrypted_content": "Earlier work"}},
		"name not type":  []any{map[string]any{"name": "compaction_trigger"}},
		"empty type":     []any{map[string]any{"type": ""}},
		"nested in text": []any{map[string]any{"role": "user", "content": "compaction_trigger"}},
	}
	for name, input := range cases {
		if responsesInputHasCompactionTrigger(input) {
			t.Fatalf("%s was mistaken for a compaction request", name)
		}
	}
}

// The trigger is a request control, not conversation content. It carries no
// role and no content, so without an explicit skip it falls through to the
// message branch and appends an empty user message. That empty message is the
// last turn BuildConversationPayload would send to M365.
func TestResponsesInputDropsTheCompactionTrigger(t *testing.T) {
	messages := responsesInputToMessages([]any{
		map[string]any{"role": "user", "content": "KEEP-THIS-TURN"},
		map[string]any{"type": "compaction_trigger"},
	})

	if len(messages) != 1 {
		t.Fatalf("message count = %d, want 1; the trigger produced a message: %#v", len(messages), messages)
	}
	if messages[0].Content != "KEEP-THIS-TURN" {
		t.Fatalf("surviving message = %q, want KEEP-THIS-TURN", messages[0].Content)
	}
}

// compaction and compaction_trigger are two different item types. The first
// carries a summary of earlier work and must survive; the second is a control
// and must not. Merging them would either lose history or add an empty turn.
func TestCompactionSummarySurvivesWhileTheTriggerDoesNot(t *testing.T) {
	messages := responsesInputToMessages([]any{
		map[string]any{"type": "compaction", "encrypted_content": "SUMMARY-OF-EARLIER-WORK"},
		map[string]any{"type": "compaction_trigger"},
	})

	if len(messages) != 1 {
		t.Fatalf("message count = %d, want 1: %#v", len(messages), messages)
	}
	if !strings.Contains(messages[0].Content, "SUMMARY-OF-EARLIER-WORK") {
		t.Fatalf("the compaction summary was dropped: %#v", messages)
	}
}
