package servers

import (
	"testing"

	"github.com/KilimcininKorOglu/M365Bridge/pkg/client"
	"github.com/KilimcininKorOglu/M365Bridge/pkg/toolcalling"
)

const completionClaim = "I created the file and ran the tests."

func TestUnverifiedClaimIsReplacedWhenNothingBacksIt(t *testing.T) {
	var empty toolcalling.Ledger
	got := withoutUnverifiedCompletionClaim(completionClaim, true, empty, nil)
	if got != unverifiedCompletionNotice {
		t.Fatalf("got %q, want the not-verified notice", got)
	}
}

func TestUnverifiedClaimIsLeftAloneWithoutTools(t *testing.T) {
	// A plain chat request declares no tools, so nothing in the answer is a
	// claim about work this server was asked to drive.
	var empty toolcalling.Ledger
	answer := "I created a summary of the release notes for you."
	if got := withoutUnverifiedCompletionClaim(answer, false, empty, nil); got != answer {
		t.Fatalf("a toolless answer was rewritten to %q", got)
	}
}

func TestUnverifiedClaimIsLeftAloneWhenTheTurnCallsATool(t *testing.T) {
	var empty toolcalling.Ledger
	calls := []client.ToolCall{{ID: "call_1", Type: "function"}}
	if got := withoutUnverifiedCompletionClaim(completionClaim, true, empty, calls); got != completionClaim {
		t.Fatalf("a turn that emitted a tool call was rewritten to %q", got)
	}
}

func TestUnverifiedClaimIsLeftAloneWhenEvidenceExists(t *testing.T) {
	ledger := buildToolLedger(decodeMessages(t, `[
		{"role":"user","content":"fix it"},
		{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"write_file","arguments":"{}"}}]},
		{"role":"tool","tool_call_id":"call_1","content":"written"}
	]`))
	if got := withoutUnverifiedCompletionClaim(completionClaim, true, ledger, nil); got != completionClaim {
		t.Fatalf("a claim backed by a tool result was rewritten to %q", got)
	}
}

func TestUnverifiedClaimLeavesAnOrdinaryToolAnswerAlone(t *testing.T) {
	var empty toolcalling.Ledger
	answer := "The repository has three packages under pkg/."
	if got := withoutUnverifiedCompletionClaim(answer, true, empty, nil); got != answer {
		t.Fatalf("an ordinary answer was rewritten to %q", got)
	}
}

func TestReplaceUnverifiedClaimUsesTheCallCount(t *testing.T) {
	var empty toolcalling.Ledger
	if got := replaceUnverifiedCompletionClaim(completionClaim, true, empty, 1); got != completionClaim {
		t.Fatalf("a turn with one parsed call was rewritten to %q", got)
	}
	if got := replaceUnverifiedCompletionClaim(completionClaim, true, empty, 0); got != unverifiedCompletionNotice {
		t.Fatalf("got %q, want the not-verified notice", got)
	}
}
