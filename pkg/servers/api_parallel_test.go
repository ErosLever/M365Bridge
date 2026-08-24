package servers

import "testing"

// An absent parallel_tool_calls means the protocol default, which allows
// parallel calls. Only an explicit false refuses them.
func TestRefusesParallelToolCalls(t *testing.T) {
	yes, no := true, false

	for _, c := range []struct {
		name  string
		field *bool
		want  bool
	}{
		{"absent", nil, false},
		{"true", &yes, false},
		{"false", &no, true},
	} {
		if got := refusesParallelToolCalls(c.field); got != c.want {
			t.Errorf("%s: refusesParallelToolCalls = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestAnthropicRefusesParallelToolCalls(t *testing.T) {
	for _, c := range []struct {
		name   string
		choice map[string]any
		want   bool
	}{
		{"no tool_choice", nil, false},
		{"tool_choice without the field", map[string]any{"type": "auto"}, false},
		{"disabled", map[string]any{"type": "auto", "disable_parallel_tool_use": true}, true},
		{"enabled", map[string]any{"type": "auto", "disable_parallel_tool_use": false}, false},
		{"wrong type", map[string]any{"disable_parallel_tool_use": "true"}, false},
	} {
		if got := anthropicRefusesParallelToolCalls(c.choice); got != c.want {
			t.Errorf("%s: anthropicRefusesParallelToolCalls = %v, want %v", c.name, got, c.want)
		}
	}
}

// The Responses path carries the refusal on the tool policy, because that is
// what reaches both the streaming and the buffered responder.
func TestResponsesPolicyForwardsOneToolCallWhenSerial(t *testing.T) {
	tools := responsesTestTools()
	policy, err := newResponsesToolPolicy(tools, "auto")
	if err != nil {
		t.Fatalf("newResponsesToolPolicy: %v", err)
	}
	if len(policy.allowedToolNames) < 2 {
		t.Fatalf("allowed tools = %v, want at least two to ask for two calls", policy.allowedToolNames)
	}
	first, second := policy.allowedToolNames[0], policy.allowedToolNames[1]
	text := "```json\n" + `{"choices":[{"message":{"role":"assistant","tool_calls":[
{"id":"call_1","type":"function","function":{"name":"` + first + `","arguments":"{}"}},
{"id":"call_2","type":"function","function":{"name":"` + second + `","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}` + "\n```"

	parallel, err := parseResponsesSimulation(text, policy)
	if err != nil {
		t.Fatalf("parseResponsesSimulation: %v", err)
	}
	if len(parallel.toolCalls) != 2 {
		t.Fatalf("tool calls = %d, want both when parallel calls are allowed", len(parallel.toolCalls))
	}

	policy.noParallel = true
	serial, err := parseResponsesSimulation(text, policy)
	if err != nil {
		t.Fatalf("parseResponsesSimulation: %v", err)
	}
	if len(serial.toolCalls) != 1 {
		t.Fatalf("tool calls = %d, want 1 when the request refused parallel calls", len(serial.toolCalls))
	}
	if serial.toolCalls[0].Function.Name != first {
		t.Errorf("kept %q, want the first call", serial.toolCalls[0].Function.Name)
	}
}
