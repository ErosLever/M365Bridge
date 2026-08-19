package servers

import (
	"testing"

	"github.com/KilimcininKorOglu/M365Bridge/pkg/client"
)

func backendSearchCall() []client.ToolCall {
	return []client.ToolCall{{
		ID:   "1a9c2bca-3b7b-4089-9f9d-365f6b28d0aa",
		Type: "function",
		Function: client.ToolCallFunction{
			Name:      "search",
			Arguments: `{"query":"Go latest stable version"}`,
		},
	}}
}

func TestBackendToolCallsAreDroppedWithoutDeclaredTools(t *testing.T) {
	// A plain chat turn that triggers M365's own web search used to reach the
	// client as a call to a "search" tool it never declared.
	calls, finish := withoutBackendToolCalls(backendSearchCall(), "tool_calls")
	if len(calls) != 0 {
		t.Fatalf("calls = %#v, want the backend call dropped", calls)
	}
	if finish != "stop" {
		t.Fatalf("finish reason = %q, want stop", finish)
	}
}

func TestBackendToolCallsResetTheAnthropicStopReason(t *testing.T) {
	_, finish := withoutBackendToolCalls(backendSearchCall(), "tool_use")
	if finish != "stop" {
		t.Fatalf("finish reason = %q, want stop", finish)
	}
}

func TestWithoutBackendToolCallsLeavesAnEmptyTurnAlone(t *testing.T) {
	calls, finish := withoutBackendToolCalls(nil, "length")
	if calls != nil {
		t.Fatalf("calls = %#v, want nil", calls)
	}
	// A truncated turn carries no backend call, so its finish reason must
	// survive: rewriting it to stop would hide the truncation.
	if finish != "length" {
		t.Fatalf("finish reason = %q, want length", finish)
	}
}

func TestWithoutBackendToolCallsKeepsAnUnrelatedFinishReason(t *testing.T) {
	_, finish := withoutBackendToolCalls(backendSearchCall(), "length")
	if finish != "length" {
		t.Fatalf("finish reason = %q, want length", finish)
	}
}
