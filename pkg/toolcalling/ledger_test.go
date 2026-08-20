package toolcalling

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBuildLedgerSeparatesAnsweredFromPending(t *testing.T) {
	ledger := BuildLedger(
		[]LedgerCall{
			{ID: "call_1", Name: "read_file", Arguments: `{"path":"main.go"}`},
			{ID: "call_2", Name: "run_tests", Arguments: `{}`},
		},
		[]LedgerResult{{ID: "call_1", Content: "package main"}},
		1,
	)

	if len(ledger.Completed) != 1 || ledger.Completed[0].Name != "read_file" {
		t.Fatalf("completed = %#v, want the answered read_file", ledger.Completed)
	}
	if ledger.Completed[0].Result != "package main" {
		t.Fatalf("result = %q, want the client's result body", ledger.Completed[0].Result)
	}
	if ledger.Completed[0].Failed {
		t.Fatal("a successful result was marked as a failure")
	}
	if len(ledger.Pending) != 1 || ledger.Pending[0].Name != "run_tests" {
		t.Fatalf("pending = %#v, want the unanswered run_tests", ledger.Pending)
	}
}

func TestBuildLedgerMarksFailedResults(t *testing.T) {
	ledger := BuildLedger(
		[]LedgerCall{{ID: "call_1", Name: "run_tests", Arguments: `{}`}},
		[]LedgerResult{{ID: "call_1", Content: "exit code 1\nFAIL github.com/example/pkg"}},
		1,
	)
	if len(ledger.Completed) != 1 || !ledger.Completed[0].Failed {
		t.Fatalf("completed = %#v, want the result marked as failed", ledger.Completed)
	}
}

func TestBuildLedgerIgnoresResultsWithoutACall(t *testing.T) {
	// A client that trimmed its history sends a result whose call is gone.
	// There is nothing to attribute it to, so it is not evidence.
	ledger := BuildLedger(nil, []LedgerResult{{ID: "call_old", Content: "sunny"}}, 0)
	if len(ledger.Completed) != 0 || len(ledger.Pending) != 0 {
		t.Fatalf("ledger = %#v, want no evidence from an orphan result", ledger)
	}
}

func TestBuildLedgerDetectsRepeatedCall(t *testing.T) {
	ledger := BuildLedger(
		[]LedgerCall{
			{ID: "call_1", Name: "read_file", Arguments: `{"path":"main.go","mode":"text"}`},
			{ID: "call_2", Name: "read_file", Arguments: `{"mode":"text","path":"main.go"}`},
		},
		[]LedgerResult{
			{ID: "call_1", Content: "package main"},
			{ID: "call_2", Content: "package main"},
		},
		2,
	)
	if !ledger.RepeatedCall {
		t.Fatal("the same call with reordered argument keys was not seen as a repeat")
	}
	if ledger.RepetitionSignature != "read_file" {
		t.Fatalf("signature = %q, want read_file", ledger.RepetitionSignature)
	}
	if ledger.RepeatedFailure {
		t.Fatal("two successful results were reported as a repeated failure")
	}
}

func TestBuildLedgerDetectsRepeatedFailureAcrossDifferentNumbers(t *testing.T) {
	ledger := BuildLedger(
		[]LedgerCall{
			{ID: "call_1", Name: "run_tests", Arguments: `{}`},
			{ID: "call_2", Name: "run_tests", Arguments: `{}`},
		},
		[]LedgerResult{
			{ID: "call_1", Content: "error: build failed after 12 seconds at line 40"},
			{ID: "call_2", Content: "error: build failed after 9 seconds at line 40"},
		},
		2,
	)
	if !ledger.RepeatedFailure {
		t.Fatal("the same failure with different durations was not recognized")
	}
}

func TestBuildLedgerKeepsDistinctFailuresApart(t *testing.T) {
	ledger := BuildLedger(
		[]LedgerCall{
			{ID: "call_1", Name: "run_tests", Arguments: `{}`},
			{ID: "call_2", Name: "run_tests", Arguments: `{}`},
		},
		[]LedgerResult{
			{ID: "call_1", Content: "error: undefined variable foo"},
			{ID: "call_2", Content: "error: permission denied opening bar"},
		},
		2,
	)
	if ledger.RepeatedFailure {
		t.Fatal("two different failures were merged into a repeated failure")
	}
}

func TestCanonicalArgumentsEqualizesKeyOrder(t *testing.T) {
	a := CanonicalArguments(`{"b":2,"a":1}`)
	b := CanonicalArguments(` {"a":1, "b":2} `)
	if a != b {
		t.Fatalf("canonical forms differ: %q vs %q", a, b)
	}
}

func TestCanonicalArgumentsLeavesNonJSONAlone(t *testing.T) {
	if got := CanonicalArguments("  ls -la  "); got != "ls -la" {
		t.Fatalf("got %q, want the trimmed original", got)
	}
}

func TestFilterRepeatedDropsOnlyThePersistentRepeat(t *testing.T) {
	call := func(name, args string) ToolCall {
		return ToolCall{ID: "call_new", Name: name, Arguments: json.RawMessage(args)}
	}

	answeredOnce := BuildLedger(
		[]LedgerCall{{ID: "call_1", Name: "read_file", Arguments: `{"path":"main.go"}`}},
		[]LedgerResult{{ID: "call_1", Content: "package main"}},
		1,
	)
	kept, dropped := answeredOnce.FilterRepeated([]ToolCall{call("read_file", `{"path":"main.go"}`)})
	if len(kept) != 1 || len(dropped) != 0 {
		t.Fatalf("first repeat was dropped: kept=%d dropped=%d", len(kept), len(dropped))
	}

	answeredTwice := BuildLedger(
		[]LedgerCall{
			{ID: "call_1", Name: "read_file", Arguments: `{"path":"main.go"}`},
			{ID: "call_2", Name: "read_file", Arguments: `{"path":"main.go"}`},
		},
		[]LedgerResult{
			{ID: "call_1", Content: "package main"},
			{ID: "call_2", Content: "package main"},
		},
		2,
	)
	kept, dropped = answeredTwice.FilterRepeated([]ToolCall{call("read_file", `{"path":"main.go"}`)})
	if len(kept) != 0 || len(dropped) != 1 {
		t.Fatalf("the third identical call survived: kept=%d dropped=%d", len(kept), len(dropped))
	}

	kept, dropped = answeredTwice.FilterRepeated([]ToolCall{call("read_file", `{"path":"other.go"}`)})
	if len(kept) != 1 || len(dropped) != 0 {
		t.Fatalf("a call with different arguments was dropped: kept=%d dropped=%d", len(kept), len(dropped))
	}
}

func TestFilterRepeatedPassesEverythingWithoutEvidence(t *testing.T) {
	var empty Ledger
	calls := []ToolCall{{Name: "read_file", Arguments: json.RawMessage(`{}`)}}
	kept, dropped := empty.FilterRepeated(calls)
	if len(kept) != 1 || dropped != nil {
		t.Fatalf("kept=%d dropped=%#v, want the call forwarded untouched", len(kept), dropped)
	}
}

func TestEvidenceNoteCarriesTheResultsAndTheFailureWarning(t *testing.T) {
	var empty Ledger
	if note := empty.EvidenceNote(); note != "" {
		t.Fatalf("an empty ledger produced %q", note)
	}

	ledger := BuildLedger(
		[]LedgerCall{
			{ID: "call_1", Name: "run_tests", Arguments: `{}`},
			{ID: "call_2", Name: "run_tests", Arguments: `{}`},
		},
		[]LedgerResult{
			{ID: "call_1", Content: "error: build failed at line 40"},
			{ID: "call_2", Content: "error: build failed at line 41"},
		},
		2,
	)
	note := ledger.EvidenceNote()
	if !strings.Contains(note, "run_tests") {
		t.Fatalf("note %q does not name the call", note)
	}
	if !strings.Contains(note, "Change the approach") {
		t.Fatalf("note %q does not warn about the repeated failure", note)
	}
}

func TestSimulatedPromptsCarryTheEvidenceNote(t *testing.T) {
	ledger := BuildLedger(
		[]LedgerCall{{ID: "call_1", Name: "read_file", Arguments: `{"path":"main.go"}`}},
		[]LedgerResult{{ID: "call_1", Content: "package main"}},
		1,
	)
	note := ledger.EvidenceNote()
	if note == "" {
		t.Fatal("a ledger with a completed call produced no note")
	}

	builders := map[string]func(string, bool, string, string) string{
		"chat completions": BuildSimulatedPrompt,
		"responses":        BuildSimulatedPromptResponses,
		"anthropic":        BuildSimulatedPromptAnthropic,
	}
	for name, build := range builders {
		withEvidence := build(`{"tools":[]}`, true, "auto", note)
		if !strings.Contains(withEvidence, "read_file") {
			t.Fatalf("%s prompt omits the completed call", name)
		}
		if !strings.Contains(withEvidence, "package main") {
			t.Fatalf("%s prompt omits the result the call returned", name)
		}
		withoutEvidence := build(`{"tools":[]}`, true, "auto", "")
		if strings.Contains(withoutEvidence, "TOOL EVIDENCE") {
			t.Fatalf("%s prompt carries an evidence section without evidence", name)
		}
	}
}

func TestCompactResultLeavesAShortResultAlone(t *testing.T) {
	short := "sunny, 24C"
	if got := compactResult(short); got != short {
		t.Fatalf("got %q, want the result untouched", got)
	}
}

func TestCompactResultKeepsTheHeadAndTheTail(t *testing.T) {
	head := "FIRST LINE OF THE LOG"
	tail := "LAST LINE OF THE LOG"
	long := head + strings.Repeat("x", maxEvidenceResult*2) + tail

	got := compactResult(long)
	if len(got) >= len(long) {
		t.Fatalf("compacted length %d, want less than %d", len(got), len(long))
	}
	if !strings.HasPrefix(got, head) {
		t.Fatalf("head was lost: %q", got[:40])
	}
	if !strings.HasSuffix(got, tail) {
		t.Fatalf("tail was lost: %q", got[len(got)-40:])
	}
	if !strings.Contains(got, "truncated") || !strings.Contains(got, "bytes") {
		t.Fatalf("the marker does not report the removed size: %q", got)
	}
}

func TestBuildLedgerCompactsTheResultButKeepsTheFailureVerdict(t *testing.T) {
	// The failing line sits in the middle, which is the part compaction drops.
	body := strings.Repeat("a", maxEvidenceResult) + "\nexit status 1\n" + strings.Repeat("b", maxEvidenceResult)
	ledger := BuildLedger(
		[]LedgerCall{{ID: "call_1", Name: "run_tests", Arguments: `{}`}},
		[]LedgerResult{{ID: "call_1", Content: body}},
		1,
	)
	if len(ledger.Completed) != 1 {
		t.Fatalf("completed = %#v, want one record", ledger.Completed)
	}
	if !ledger.Completed[0].Failed {
		t.Fatal("the failure verdict was computed on the compacted text")
	}
	if len(ledger.Completed[0].Result) > len(body) {
		t.Fatal("the result was not compacted")
	}
	if len(ledger.Completed[0].Result) > maxEvidenceResult+200 {
		t.Fatalf("compacted result is %d bytes, want near %d", len(ledger.Completed[0].Result), maxEvidenceResult)
	}
}

func TestFailureSignalRecognizesEveryExitForm(t *testing.T) {
	failing := []string{"exit status 1", "exit code: 2", "exit code 1", "exit=127"}
	for _, text := range failing {
		if !failureSignal.MatchString(text) {
			t.Fatalf("%q was not recognized as a failure", text)
		}
	}
	if failureSignal.MatchString("exit code 0") {
		t.Fatal("a zero exit code was reported as a failure")
	}
}

func TestEvidenceNoteStaysBoundedForAHugeResult(t *testing.T) {
	// The note is restated in the prompt on every request of a client-driven
	// loop, so an unbounded result would grow the prompt without limit.
	huge := strings.Repeat("line of build output\n", 3000)
	ledger := BuildLedger(
		[]LedgerCall{{ID: "call_1", Name: "run_tests", Arguments: `{}`}},
		[]LedgerResult{{ID: "call_1", Content: huge + "exit status 1\n" + huge}},
		1,
	)
	note := ledger.EvidenceNote()
	if len(note) > maxEvidenceResult*2 {
		t.Fatalf("note is %d bytes for a %d byte result, want it bounded", len(note), len(huge)*2)
	}
	if !strings.Contains(note, "truncated") {
		t.Fatalf("note does not report the truncation: %.200q", note)
	}
}

// The request-local coding-tool loop and the client-driven ledger both ask "is
// this the same call", so they share one answer. A model that re-emits a call
// with its JSON keys in another order is repeating it.
func TestCallSignatureIgnoresKeyOrderAndWhitespace(t *testing.T) {
	same := []string{
		`{"path":"a.go","limit":10}`,
		`{"limit":10,"path":"a.go"}`,
		"{ \"path\" : \"a.go\" , \"limit\" : 10 }",
		"\n\t{\"limit\":10,\"path\":\"a.go\"}\n",
	}
	want := CallSignature("read_file", same[0])
	for _, arguments := range same[1:] {
		if got := CallSignature("read_file", arguments); got != want {
			t.Errorf("CallSignature(%q) = %q, want %q", arguments, got, want)
		}
	}
}

func TestCallSignatureSeparatesDifferentCalls(t *testing.T) {
	base := CallSignature("read_file", `{"path":"a.go"}`)
	for _, other := range []struct{ name, arguments string }{
		{"read_file", `{"path":"b.go"}`},
		{"write_file", `{"path":"a.go"}`},
		{"read_file", ``},
	} {
		if got := CallSignature(other.name, other.arguments); got == base {
			t.Errorf("%s/%s collides with the base signature", other.name, other.arguments)
		}
	}
}

// A non-JSON argument string is still a stable identity, so it must not collapse
// every such call into one signature.
func TestCallSignatureKeepsNonJSONArgumentsDistinct(t *testing.T) {
	if CallSignature("shell_command", "ls -la") == CallSignature("shell_command", "rm -rf /") {
		t.Error("two different non-JSON argument strings share a signature")
	}
}
