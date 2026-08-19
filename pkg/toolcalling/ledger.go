package toolcalling

import (
	"encoding/json"
	"regexp"
	"strings"
)

// maxFailureSignature caps the normalized failure text kept for comparison.
// A stack trace can be arbitrarily long and only its head distinguishes one
// failure from another.
const maxFailureSignature = 500

// failureSignal matches the wording a tool result uses to report that the tool
// did not do what it was asked. The ledger uses it to tell an answered call
// apart from an answered-but-failed one, which is what makes a repeat worth
// reporting to the model.
var failureSignal = regexp.MustCompile(`(?i)\b(exit\s*code\s*[1-9]|error|failed|failure|exception|traceback|timed?\s*out|timeout|permission denied|not found|refused)\b`)

// digitRun collapses the numbers inside a failure message so that two runs of
// the same failure compare equal even when line numbers, durations or process
// ids differ.
var digitRun = regexp.MustCompile(`\d+`)

// ToolEvidence is one tool call together with the result the client returned
// for it, if any.
type ToolEvidence struct {
	ID        string `json:"id,omitempty"`
	Name      string `json:"name"`
	Arguments string `json:"arguments,omitempty"`
	Result    string `json:"result,omitempty"`
	Failed    bool   `json:"failed,omitempty"`
}

// Ledger is the evidence a client-driven tool loop has accumulated so far. It
// is rebuilt from the incoming message history on every request, because the
// server holds no state between the turns of such a loop.
type Ledger struct {
	// Completed holds the calls whose results are already in the history.
	Completed []ToolEvidence
	// Pending holds the calls the client announced but has not answered.
	Pending []ToolEvidence
	// Rounds counts the assistant turns that announced at least one call.
	Rounds int
	// RepeatedCall reports that the same call was issued and answered more
	// than once.
	RepeatedCall bool
	// RepeatedFailure reports that the same call failed the same way more than
	// once, which means another attempt is unlikely to behave differently.
	RepeatedFailure bool
	// RepetitionSignature names the call behind RepeatedCall or
	// RepeatedFailure, for logging.
	RepetitionSignature string
}

// LedgerCall is one announced tool call, in the provider-independent shape the
// ledger consumes.
type LedgerCall struct {
	ID        string
	Name      string
	Arguments string
}

// LedgerResult is one tool result, in the provider-independent shape the ledger
// consumes.
type LedgerResult struct {
	ID      string
	Content string
}

// CanonicalArguments normalizes a tool argument string so that two calls that
// differ only in key order or whitespace compare equal. Text that is not valid
// JSON is compared after trimming, because a non-JSON argument string is still
// a stable identity for the call.
func CanonicalArguments(arguments string) string {
	trimmed := strings.TrimSpace(arguments)
	if trimmed == "" {
		return ""
	}
	var decoded any
	if err := json.Unmarshal([]byte(trimmed), &decoded); err != nil {
		return trimmed
	}
	// Re-marshaling an any sorts object keys, which is exactly the
	// normalization wanted here.
	encoded, err := json.Marshal(decoded)
	if err != nil {
		return trimmed
	}
	return string(encoded)
}

// normalizeFailure reduces a failure message to a signature that ignores the
// numbers inside it and its tail.
func normalizeFailure(result string) string {
	normalized := digitRun.ReplaceAllString(strings.ToLower(strings.TrimSpace(result)), "#")
	if len(normalized) > maxFailureSignature {
		normalized = normalized[:maxFailureSignature]
	}
	return normalized
}

// callSignature identifies a call by what it does, not by the id the client
// assigned it. Two turns of the same loop use different ids for the same work.
func callSignature(name, arguments string) string {
	return name + "\x00" + CanonicalArguments(arguments)
}

// BuildLedger reconstructs the evidence of a client-driven tool loop from the
// calls and results found in the incoming history. Calls and results are
// matched by id; a result whose call is no longer in the history (a client that
// trimmed its context) is ignored, because there is nothing to attribute it to.
//
// The caller collects calls and results from its own message shape, which keeps
// this package independent of the provider payload types.
func BuildLedger(calls []LedgerCall, results []LedgerResult, rounds int) Ledger {
	resultByID := make(map[string]string, len(results))
	for _, result := range results {
		if result.ID != "" {
			resultByID[result.ID] = result.Content
		}
	}

	ledger := Ledger{Rounds: rounds}
	seenCall := make(map[string]bool, len(calls))
	seenFailure := make(map[string]bool, len(calls))

	for _, call := range calls {
		evidence := ToolEvidence{ID: call.ID, Name: call.Name, Arguments: call.Arguments}
		result, answered := resultByID[call.ID]
		if !answered {
			ledger.Pending = append(ledger.Pending, evidence)
			continue
		}
		evidence.Result = result
		evidence.Failed = failureSignal.MatchString(result)
		ledger.Completed = append(ledger.Completed, evidence)

		signature := callSignature(call.Name, call.Arguments)
		if seenCall[signature] {
			ledger.RepeatedCall = true
			if ledger.RepetitionSignature == "" {
				ledger.RepetitionSignature = call.Name
			}
		}
		seenCall[signature] = true

		if !evidence.Failed {
			continue
		}
		failureKey := signature + "\x00" + normalizeFailure(result)
		if seenFailure[failureKey] {
			ledger.RepeatedFailure = true
			ledger.RepetitionSignature = call.Name
		}
		seenFailure[failureKey] = true
	}

	return ledger
}

// CompletedCount reports how many times a call with this name and these
// arguments already has a result in the history.
func (l Ledger) CompletedCount(name, arguments string) int {
	signature := callSignature(name, arguments)
	count := 0
	for _, evidence := range l.Completed {
		if callSignature(evidence.Name, evidence.Arguments) == signature {
			count++
		}
	}
	return count
}

// FilterRepeated splits parsed tool calls into the ones worth forwarding and
// the ones the model is issuing for at least the third time.
//
// The first repeat is allowed through: reading a file back after writing it, or
// re-running the tests after a change, are ordinary and the arguments are
// identical both times. A third identical call means the model is not reacting
// to the result it already has.
func (l Ledger) FilterRepeated(calls []ToolCall) (kept, dropped []ToolCall) {
	if len(l.Completed) == 0 {
		return calls, nil
	}
	for _, call := range calls {
		if l.CompletedCount(call.Name, string(call.Arguments)) >= 2 {
			dropped = append(dropped, call)
			continue
		}
		kept = append(kept, call)
	}
	return kept, dropped
}

// RepeatedCallsNotice replaces the answer text when every tool call of a turn
// was dropped as a settled repeat. The parser clears the content whenever tool
// calls are present, so without a substitute the client would receive a turn
// with neither calls nor an answer.
const RepeatedCallsNotice = "The tools requested in this turn have already run with the same arguments, and their results are in the conversation above. Answer from those results instead of calling them again."

// EvidenceNote renders the completed calls as a compact instruction for the
// prompt, so the model treats a result it already has as settled instead of
// asking for it again. It returns an empty string when there is no evidence.
func (l Ledger) EvidenceNote() string {
	if len(l.Completed) == 0 {
		return ""
	}

	encoded, err := json.Marshal(l.Completed)
	if err != nil {
		return ""
	}

	var note strings.Builder
	note.WriteString("\n\nTOOL EVIDENCE FROM THIS CONVERSATION\n")
	note.WriteString("These tool calls already ran and their results are final:\n")
	note.Write(encoded)
	note.WriteString("\nTreat each result as authoritative. Do not call a tool again with the same name and the same arguments unless the result was a failure you are addressing differently.")
	if l.RepeatedFailure {
		note.WriteString("\nThe call named ")
		note.WriteString(l.RepetitionSignature)
		note.WriteString(" already failed the same way more than once. Change the approach or report the failure to the user instead of repeating it.")
	}
	return note.String()
}
