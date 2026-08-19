package servers

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/KilimcininKorOglu/M365Bridge/pkg/toolcalling"
)

// settledLedger builds a ledger in which run_tests has already run and been
// answered the given number of times inside one user turn.
func settledLedger(t *testing.T, times int) toolcalling.Ledger {
	t.Helper()
	var sb strings.Builder
	sb.WriteString(`[{"role":"user","content":"fix the build"}`)
	for i := range times {
		id := "call_" + string(rune('0'+i))
		sb.WriteString(`,{"role":"assistant","content":null,"tool_calls":[{"id":"` + id +
			`","type":"function","function":{"name":"run_tests","arguments":"{\"pkg\":\"./...\"}"}}]}`)
		sb.WriteString(`,{"role":"tool","tool_call_id":"` + id + `","content":"exit code 1"}`)
	}
	sb.WriteString("]")
	return buildToolLedger(decodeMessages(t, sb.String()))
}

func simWithRunTests() toolcalling.SimulatedResult {
	return toolcalling.SimulatedResult{
		HasPayload:   true,
		FinishReason: "tool_calls",
		ToolCalls: []toolcalling.ToolCall{{
			ID:        "call_new",
			Name:      "run_tests",
			Arguments: json.RawMessage(`{"pkg":"./..."}`),
		}},
	}
}

func TestDropSettledToolCallsKeepsTheFirstRepeat(t *testing.T) {
	got := dropSettledToolCalls(settledLedger(t, 1), "auto", simWithRunTests())
	if len(got.ToolCalls) != 1 {
		t.Fatalf("tool calls = %d, want the first repeat forwarded", len(got.ToolCalls))
	}
	if got.FinishReason != "tool_calls" {
		t.Fatalf("finish reason = %q, want tool_calls", got.FinishReason)
	}
}

func TestDropSettledToolCallsStopsThePersistentRepeat(t *testing.T) {
	got := dropSettledToolCalls(settledLedger(t, 2), "auto", simWithRunTests())
	if len(got.ToolCalls) != 0 {
		t.Fatalf("tool calls = %d, want the third identical call dropped", len(got.ToolCalls))
	}
	if got.FinishReason != "stop" {
		t.Fatalf("finish reason = %q, want stop once nothing is left to call", got.FinishReason)
	}
	if got.Content != toolcalling.RepeatedCallsNotice {
		t.Fatalf("content = %q, want the substitute notice instead of an empty turn", got.Content)
	}
	if len(got.DroppedCalls) != 0 {
		t.Fatalf("dropped calls = %#v, want no corrective re-ask triggered", got.DroppedCalls)
	}
}

func TestDropSettledToolCallsKeepsExistingAnswerText(t *testing.T) {
	sim := simWithRunTests()
	sim.Content = "The build is already green."
	got := dropSettledToolCalls(settledLedger(t, 2), "auto", sim)
	if got.Content != "The build is already green." {
		t.Fatalf("content = %q, want the model's own text preserved", got.Content)
	}
}

func TestDropSettledToolCallsHonorsAForcedToolChoice(t *testing.T) {
	// The caller demanded this call by name, so refusing it would contradict
	// the request even though the result is already in hand.
	for _, choice := range []string{"run_tests", "required", "any"} {
		got := dropSettledToolCalls(settledLedger(t, 2), choice, simWithRunTests())
		if len(got.ToolCalls) != 1 {
			t.Fatalf("tool_choice %q: tool calls = %d, want the demanded call forwarded", choice, len(got.ToolCalls))
		}
	}
}

func TestDropSettledToolCallsLeavesADifferentCallAlone(t *testing.T) {
	sim := simWithRunTests()
	sim.ToolCalls[0].Arguments = json.RawMessage(`{"pkg":"./pkg/servers"}`)
	got := dropSettledToolCalls(settledLedger(t, 2), "auto", sim)
	if len(got.ToolCalls) != 1 {
		t.Fatalf("tool calls = %d, want a call with different arguments forwarded", len(got.ToolCalls))
	}
}

func TestParseResponsesSimulationDropsASettledRepeat(t *testing.T) {
	policy, err := newResponsesToolPolicy(responsesTestTools(), "auto")
	if err != nil {
		t.Fatalf("newResponsesToolPolicy: %v", err)
	}
	name := policy.allowedToolNames[0]

	var sb strings.Builder
	sb.WriteString(`[{"role":"user","content":"go"}`)
	for i := range 2 {
		id := "call_" + string(rune('0'+i))
		sb.WriteString(`,{"role":"assistant","content":null,"tool_calls":[{"id":"` + id +
			`","type":"function","function":{"name":"` + name + `","arguments":"{}"}}]}`)
		sb.WriteString(`,{"role":"tool","tool_call_id":"` + id + `","content":"done"}`)
	}
	sb.WriteString("]")
	policy.ledger = buildToolLedger(decodeMessages(t, sb.String()))

	text := "```json\n" + `{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"call_x","type":"function","function":{"name":"` +
		name + `","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}` + "\n```"

	result, err := parseResponsesSimulation(text, policy)
	if err != nil {
		t.Fatalf("parseResponsesSimulation: %v", err)
	}
	if len(result.toolCalls) != 0 {
		t.Fatalf("tool calls = %d, want the settled repeat dropped on the Responses path", len(result.toolCalls))
	}
	if result.finishReason != "stop" {
		t.Fatalf("finish reason = %q, want stop", result.finishReason)
	}
	if result.content != toolcalling.RepeatedCallsNotice {
		t.Fatalf("content = %q, want the substitute notice", result.content)
	}
}
