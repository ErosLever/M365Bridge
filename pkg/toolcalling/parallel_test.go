package toolcalling

import "testing"

func twoCallTools() []ToolDef {
	return []ToolDef{
		{Type: "function", Function: ToolDefFunc{Name: "read_file"}},
		{Type: "function", Function: ToolDefFunc{Name: "run_tests"}},
	}
}

const twoCallChatPayload = "```json\n" + `{"choices":[{"message":{"role":"assistant","tool_calls":[
{"id":"call_1","type":"function","function":{"name":"read_file","arguments":"{}"}},
{"id":"call_2","type":"function","function":{"name":"run_tests","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}` + "\n```"

const twoCallAnthropicPayload = "```json\n" + `{"role":"assistant","content":[
{"type":"tool_use","id":"toolu_1","name":"read_file","input":{}},
{"type":"tool_use","id":"toolu_2","name":"run_tests","input":{}}],"stop_reason":"tool_use"}` + "\n```"

// A caller that sent parallel_tool_calls: false executes one call per turn, so
// a second call in the same response is never run. It used to reach the client
// anyway, because nothing read the field.
func TestSerialCallerReceivesOneChatToolCall(t *testing.T) {
	contracts := ContractsFor(twoCallTools()).WithoutParallel(true)

	got := ParseSimulatedResponse(twoCallChatPayload, []string{"read_file", "run_tests"}, contracts)

	if len(got.ToolCalls) != 1 {
		t.Fatalf("tool calls = %d, want 1", len(got.ToolCalls))
	}
	// The model emits the calls in the order it wants them run, so the first
	// one is the one to keep.
	if got.ToolCalls[0].Name != "read_file" {
		t.Errorf("kept %q, want the first call", got.ToolCalls[0].Name)
	}
}

func TestParallelCallerReceivesEveryChatToolCall(t *testing.T) {
	contracts := ContractsFor(twoCallTools()).WithoutParallel(false)

	got := ParseSimulatedResponse(twoCallChatPayload, []string{"read_file", "run_tests"}, contracts)

	if len(got.ToolCalls) != 2 {
		t.Fatalf("tool calls = %d, want both", len(got.ToolCalls))
	}
}

func TestSerialCallerReceivesOneResponsesToolCall(t *testing.T) {
	contracts := ContractsFor(twoCallTools()).WithoutParallel(true)

	got := ParseSimulatedResponseResponses(twoCallChatPayload, []string{"read_file", "run_tests"}, contracts)

	if len(got.ToolCalls) != 1 {
		t.Fatalf("tool calls = %d, want 1", len(got.ToolCalls))
	}
}

// Anthropic spells the same constraint disable_parallel_tool_use, and it
// reaches the parser through the same contract.
func TestSerialCallerReceivesOneAnthropicToolCall(t *testing.T) {
	contracts := ContractsFor(twoCallTools()).WithoutParallel(true)

	got := ParseSimulatedResponseAnthropic(twoCallAnthropicPayload, []string{"read_file", "run_tests"}, contracts)

	if len(got.ToolCalls) != 1 {
		t.Fatalf("tool calls = %d, want 1", len(got.ToolCalls))
	}
	if got.ToolCalls[0].Name != "read_file" {
		t.Errorf("kept %q, want the first call", got.ToolCalls[0].Name)
	}
}

func TestParallelCallerReceivesEveryAnthropicToolCall(t *testing.T) {
	contracts := ContractsFor(twoCallTools()).WithoutParallel(false)

	got := ParseSimulatedResponseAnthropic(twoCallAnthropicPayload, []string{"read_file", "run_tests"}, contracts)

	if len(got.ToolCalls) != 2 {
		t.Fatalf("tool calls = %d, want both", len(got.ToolCalls))
	}
}

// A single call is what a serial caller asked for, so it passes through
// untouched rather than being rebuilt.
func TestSerialCallerKeepsALoneToolCall(t *testing.T) {
	contracts := ContractsFor(twoCallTools()).WithoutParallel(true)
	lone := "```json\n" + `{"choices":[{"message":{"role":"assistant","tool_calls":[
{"id":"call_1","type":"function","function":{"name":"run_tests","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}` + "\n```"

	got := ParseSimulatedResponse(lone, []string{"read_file", "run_tests"}, contracts)

	if len(got.ToolCalls) != 1 || got.ToolCalls[0].Name != "run_tests" {
		t.Fatalf("tool calls = %#v, want the lone call untouched", got.ToolCalls)
	}
}
