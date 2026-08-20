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

// The item shape alone is not enough: the stream announces the item, then
// streams its input, then announces it again. A custom tool that streams its
// input as function-call arguments, or under an id no announced item carries,
// reaches a client that can never assemble the call.
func TestResponsesCustomToolStreamsItsInputUnderTheAnnouncedItemID(t *testing.T) {
	types := customAndFunctionToolTypes()
	call := toolCall("run_script", "echo hi")
	added := buildResponsesToolCallItem("call_abc", call, types, "in_progress")

	events := responsesToolInputEvents("call_abc", call, types, 3)

	if len(events) != 2 {
		t.Fatalf("events = %#v, want a delta and a done", events)
	}
	if events[0].name != "response.custom_tool_call_input.delta" ||
		events[1].name != "response.custom_tool_call_input.done" {
		t.Fatalf("custom tool input streamed as %q then %q", events[0].name, events[1].name)
	}
	for _, event := range events {
		if event.data["item_id"] != added["id"] {
			t.Fatalf("%s named item %v, but the announced item is %v", event.name, event.data["item_id"], added["id"])
		}
		if event.data["output_index"] != 3 {
			t.Fatalf("%s output_index = %v, want 3", event.name, event.data["output_index"])
		}
	}
	if events[0].data["delta"] != "echo hi" {
		t.Fatalf("delta = %v, want the raw input", events[0].data["delta"])
	}
	if events[1].data["input"] != "echo hi" {
		t.Fatalf("input = %v, want the raw input", events[1].data["input"])
	}
}

func TestResponsesFunctionToolStreamsArguments(t *testing.T) {
	types := customAndFunctionToolTypes()
	call := toolCall("get_weather", `{"city":"Istanbul"}`)

	events := responsesToolInputEvents("call_abc", call, types, 0)

	if len(events) != 2 ||
		events[0].name != "response.function_call_arguments.delta" ||
		events[1].name != "response.function_call_arguments.done" {
		t.Fatalf("function tool input streamed as %#v", events)
	}
	if events[1].data["arguments"] != `{"city":"Istanbul"}` || events[1].data["name"] != "get_weather" {
		t.Fatalf("done event lost detail: %#v", events[1].data)
	}
	// A function call item is announced under the call id itself.
	if events[0].data["item_id"] != "call_abc" {
		t.Fatalf("item_id = %v, want the call id", events[0].data["item_id"])
	}
}

// A tool_search call carries its query inside the item, so streaming an input
// event for it would announce input the item never had.
func TestResponsesToolSearchStreamsNoInput(t *testing.T) {
	types := responsesToolTypes([]toolcalling.ToolDef{{Type: "tool_search", Name: "search"}})

	if events := responsesToolInputEvents("call_abc", toolCall("search", `{"query":"go"}`), types, 0); len(events) != 0 {
		t.Fatalf("tool_search streamed %#v, want nothing", events)
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

// A client that declares one name twice, freeform and as a function, must get
// the freeform shape; a grammar body emitted as function_call arguments is not
// the JSON a client parses there.
func TestDuplicateToolNamePrefersTheCustomDeclaration(t *testing.T) {
	custom := toolcalling.ToolDef{Type: "custom", Name: "apply_patch"}
	function := toolcalling.ToolDef{Type: "function", Name: "apply_patch"}

	if got := responsesToolTypes([]toolcalling.ToolDef{custom, function})["apply_patch"]; got != "custom" {
		t.Errorf("custom declared first lost to %q", got)
	}
	if got := responsesToolTypes([]toolcalling.ToolDef{function, custom})["apply_patch"]; got != "custom" {
		t.Errorf("custom declared second lost to %q", got)
	}
}
