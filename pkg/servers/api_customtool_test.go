package servers

import (
	"strings"
	"testing"

	"github.com/KilimcininKorOglu/M365Bridge/pkg/client"
	"github.com/KilimcininKorOglu/M365Bridge/pkg/toolcalling"
)

func customAndFunctionToolTypes() map[string]string {
	return responsesToolTypes([]toolcalling.ToolDef{
		{Type: "custom", Name: "run_script"},
		{Type: "function", Name: "get_weather"},
	})
}

func toolCall(name, arguments string) client.ToolCall {
	return client.ToolCall{
		ID:   "call_abc",
		Type: "function",
		Function: client.ToolCallFunction{
			Name:      name,
			Arguments: arguments,
		},
	}
}

func TestResponsesCustomToolEmitsItsOwnItemType(t *testing.T) {
	item := buildResponsesToolCallItem("call_abc", toolCall("run_script", "echo hi"), customAndFunctionToolTypes(), "completed")

	if item["type"] != "custom_tool_call" {
		t.Fatalf("custom tool emitted as %q", item["type"])
	}
	// A custom tool takes free-form input, not JSON arguments.
	if item["input"] != "echo hi" {
		t.Fatalf("input = %v, want the raw text", item["input"])
	}
	if _, present := item["arguments"]; present {
		t.Fatalf("custom tool item carries an arguments field: %#v", item)
	}
	if item["call_id"] != "call_abc" {
		t.Fatalf("call_id = %v, want the original call id", item["call_id"])
	}
	if id, _ := item["id"].(string); !strings.HasPrefix(id, "ctc_") {
		t.Fatalf("item id = %q, want a ctc_ prefix", id)
	}
}

func TestResponsesCustomToolItemIDIsStableAcrossEvents(t *testing.T) {
	// The streaming path builds the item twice. A client correlates
	// output_item.added with output_item.done by the item id, so a fresh id on
	// the second build would orphan the first event.
	types := customAndFunctionToolTypes()
	added := buildResponsesToolCallItem("call_abc", toolCall("run_script", "echo hi"), types, "in_progress")
	done := buildResponsesToolCallItem("call_abc", toolCall("run_script", "echo hi"), types, "completed")

	if added["id"] != done["id"] {
		t.Fatalf("item id changed between events: %v then %v", added["id"], done["id"])
	}
	if added["input"] != "" {
		t.Fatalf("in-progress item leaked input %v", added["input"])
	}
}

func TestResponsesFunctionToolKeepsFunctionCallShape(t *testing.T) {
	item := buildResponsesToolCallItem("call_abc", toolCall("get_weather", `{"city":"Istanbul"}`), customAndFunctionToolTypes(), "completed")

	if item["type"] != "function_call" {
		t.Fatalf("function tool emitted as %q", item["type"])
	}
	if item["arguments"] != `{"city":"Istanbul"}` {
		t.Fatalf("arguments = %v", item["arguments"])
	}
}

func TestResponsesInputReadsCustomToolHistory(t *testing.T) {
	messages := responsesInputToMessages([]any{
		map[string]any{"type": "custom_tool_call", "call_id": "ctc_1", "name": "run_script", "input": "echo hi"},
		map[string]any{"type": "custom_tool_call_output", "call_id": "ctc_1", "output": "hi"},
	})

	if len(messages) != 2 {
		t.Fatalf("got %d messages, want the call and its result", len(messages))
	}
	if !strings.Contains(messages[0].Content, "echo hi") {
		t.Fatalf("custom call input was lost: %q", messages[0].Content)
	}
	if !strings.Contains(messages[1].Content, "hi") {
		t.Fatalf("custom call output was lost: %q", messages[1].Content)
	}
	if err := validateToolResultMessages(messages); err != nil {
		t.Fatalf("a matching custom tool pair was rejected: %v", err)
	}
}

func TestFunctionCallProgressReachesTheModelWithoutAnsweringTheCall(t *testing.T) {
	messages := responsesInputToMessages([]any{
		map[string]any{"type": "function_call", "call_id": "fc_1", "name": "run_tests", "arguments": "{}"},
		map[string]any{"type": "function_call_progress", "call_id": "fc_1", "phase": "running", "message": "compiling packages", "output": "3 of 9 done"},
	})

	if len(messages) != 2 {
		t.Fatalf("messages = %d, want the call and its progress note", len(messages))
	}
	progress := messages[1]
	if progress.Role != "user" {
		t.Fatalf("progress role = %q, want user so it is not read as a result", progress.Role)
	}
	if !strings.Contains(progress.Content, "compiling packages") || !strings.Contains(progress.Content, "3 of 9 done") {
		t.Fatalf("progress content lost detail: %q", progress.Content)
	}
	if len(progress.ToolResults) != 0 {
		t.Fatalf("progress produced tool results %#v, want none", progress.ToolResults)
	}

	// The call is still pending: nothing answered it.
	ledger := buildToolLedger(messages)
	if len(ledger.Completed) != 0 {
		t.Fatalf("completed = %#v, want the call still unanswered", ledger.Completed)
	}
	if len(ledger.Pending) != 1 {
		t.Fatalf("pending = %#v, want the announced call", ledger.Pending)
	}
	if err := validateToolResultMessages(messages); err != nil {
		t.Fatalf("a progress note was rejected as a bad tool result: %v", err)
	}
}

func TestFunctionCallProgressWithoutTheRequiredFieldsIsDropped(t *testing.T) {
	messages := responsesInputToMessages([]any{
		map[string]any{"type": "function_call_progress", "call_id": "", "message": "working"},
		map[string]any{"type": "function_call_progress", "call_id": "fc_1", "message": "   "},
	})
	// Nothing survived, so the converter falls back to one empty user message.
	if len(messages) != 1 || messages[0].Content != "" || messages[0].ToolProgress {
		t.Fatalf("messages = %#v, want both incomplete items dropped", messages)
	}
}
