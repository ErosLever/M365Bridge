package servers

import (
	"testing"

	"github.com/KilimcininKorOglu/M365Bridge/pkg/client"
)

func goalUserItem(text string) map[string]any {
	return map[string]any{
		"role": "user",
		"content": []any{
			map[string]any{"type": "input_text", "text": text},
		},
	}
}

func updateGoalCall(callID string) map[string]any {
	return map[string]any{
		"type":      "function_call",
		"call_id":   callID,
		"name":      updateGoalToolName,
		"arguments": `{"status":"in_progress"}`,
	}
}

func updateGoalOutput(callID, status string) map[string]any {
	return map[string]any{
		"type":    "function_call_output",
		"call_id": callID,
		"output":  `{"goal":{"status":"` + status + `"}}`,
	}
}

func TestGoalContinuationIsClosedWithoutTheMarker(t *testing.T) {
	if responsesGoalContinuationOpen([]any{goalUserItem("plain question")}) {
		t.Fatal("a request with no goal marker was read as an open goal")
	}
	if responsesGoalContinuationOpen("a plain string input") {
		t.Fatal("a string input was read as an open goal")
	}
	if responsesGoalContinuationOpen(nil) {
		t.Fatal("a nil input was read as an open goal")
	}
}

func TestGoalContinuationIsOpenWhileNoUpdateReportsClosure(t *testing.T) {
	input := []any{
		goalUserItem(goalContextMarker + "ship the parser</codex_internal_context>"),
		updateGoalCall("call_1"),
		updateGoalOutput("call_1", "in_progress"),
	}

	if !responsesGoalContinuationOpen(input) {
		t.Fatal("a goal still in progress was read as closed")
	}
}

func TestGoalContinuationClosesOnCompleteAndOnBlocked(t *testing.T) {
	for _, status := range []string{"complete", "blocked"} {
		input := []any{
			goalUserItem(goalContextMarker + "ship the parser</codex_internal_context>"),
			updateGoalCall("call_1"),
			updateGoalOutput("call_1", status),
		}
		if responsesGoalContinuationOpen(input) {
			t.Errorf("status %q left the goal open", status)
		}
	}
}

// An update_goal output that belongs to no call in this goal must not close it,
// and neither must some other tool's output that happens to carry the shape.
func TestGoalContinuationIgnoresAnUnrelatedOutput(t *testing.T) {
	input := []any{
		goalUserItem(goalContextMarker + "ship the parser</codex_internal_context>"),
		updateGoalCall("call_1"),
		map[string]any{"type": "function_call", "call_id": "call_2", "name": "read_file"},
		map[string]any{"type": "function_call_output", "call_id": "call_2", "output": `{"goal":{"status":"complete"}}`},
	}

	if !responsesGoalContinuationOpen(input) {
		t.Fatal("another tool's output closed the goal")
	}
}

// A later user item without the marker is a new request, so the earlier goal no
// longer governs this turn.
func TestGoalContinuationEndsAtANewUserRequest(t *testing.T) {
	input := []any{
		goalUserItem(goalContextMarker + "ship the parser</codex_internal_context>"),
		updateGoalCall("call_1"),
		updateGoalOutput("call_1", "in_progress"),
		goalUserItem("something else entirely"),
	}

	if responsesGoalContinuationOpen(input) {
		t.Fatal("a new user request stayed inside the earlier goal")
	}
}

func TestGoalContinuationReadsACustomToolReport(t *testing.T) {
	input := []any{
		goalUserItem(goalContextMarker + "ship the parser</codex_internal_context>"),
		map[string]any{"type": "custom_tool_call", "call_id": "ctc_1", "name": updateGoalToolName},
		map[string]any{"type": "custom_tool_call_output", "call_id": "ctc_1", "output": `{"goal":{"status":"complete"}}`},
	}

	if responsesGoalContinuationOpen(input) {
		t.Fatal("a custom tool report did not close the goal")
	}
}

func TestMessagePhaseFollowsTheToolCallsAndTheGoal(t *testing.T) {
	calls := []client.ToolCall{{ID: "call_1"}}

	if got := responsesMessagePhase(false, nil); got != "final_answer" {
		t.Errorf("a plain answer = %q", got)
	}
	if got := responsesMessagePhase(false, calls); got != "commentary" {
		t.Errorf("a tool-calling turn = %q", got)
	}
	if got := responsesMessagePhase(true, nil); got != "commentary" {
		t.Errorf("a turn inside an open goal = %q", got)
	}
	if got := responsesMessagePhase(true, calls); got != "commentary" {
		t.Errorf("a tool-calling turn inside an open goal = %q", got)
	}
}
