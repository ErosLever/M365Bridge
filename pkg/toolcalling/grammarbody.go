package toolcalling

import (
	"encoding/json"
	"regexp"
	"strings"
)

// A grammar-constrained tool takes a raw body rather than a JSON argument
// object: Codex code mode declares `exec` that way, and `apply_patch` behaves
// the same. M365 only accepts JSON arguments, so the body travels through a
// single `input` field. The model sometimes emits that bridge envelope itself,
// unfenced:
//
//	{"input":"shell({cmd:\"cat README.md\"}); text(plan);"}
//
// or the bare body with no envelope at all:
//
//	shell({cmd:"cat README.md"}); text(plan);
//
// Neither carries a fence, so the envelope scan never claims them and they
// reach the client as a wall of escaped source where a tool call belongs.

// codeModeHostCall matches a statement-position call to one of code mode's host
// functions. Those names are the runtime's own API, so a program calling them
// is a body rather than prose about code.
var codeModeHostCall = regexp.MustCompile(
	`(?m)^[ \t]*(?:const|let|var|await|return)?[ \t]*(?:[A-Za-z_$][A-Za-z0-9_$]*[ \t]*=[ \t]*(?:await[ \t]+)?)?` +
		`(shell|text|image|apply_patch|update_plan|read_file|write_file|wait|exec)[ \t]*\(`,
)

// GrammarToolNames returns the declared tools that take a raw body, in the
// order they were declared. A request carries at most a couple of these.
func GrammarToolNames(tools []ToolDef) []string {
	var out []string
	for i := range tools {
		if tools[i].Type != "custom" {
			continue
		}
		if name := ToolName(&tools[i]); name != "" {
			out = append(out, name)
		}
	}
	return out
}

// BridgeEnvelopeBody extracts the body from a bare `{"input": "..."}` object
// that is the whole of the text. Only a lone envelope is claimed: one embedded
// in prose is far more likely to be the model quoting JSON than issuing a call.
func BridgeEnvelopeBody(text string) (string, bool) {
	trimmed := strings.TrimSpace(text)
	if !strings.HasPrefix(trimmed, "{") || !strings.HasSuffix(trimmed, "}") {
		return "", false
	}
	var envelope map[string]json.RawMessage
	if json.Unmarshal([]byte(trimmed), &envelope) != nil {
		return "", false
	}
	raw, ok := envelope["input"]
	if !ok || len(envelope) != 1 {
		return "", false
	}
	var body string
	if json.Unmarshal(raw, &body) != nil || strings.TrimSpace(body) == "" {
		return "", false
	}
	return body, true
}

// CodeModeSourceCall detects a bare code-mode program: the body of an `exec`
// call that lost both its fence and its bridge envelope.
//
// The signature is a call to one of code mode's own host functions at statement
// position, and the text must end like source rather than like a sentence.
// Requiring both keeps prose and answers that merely quote code out.
func CodeModeSourceCall(text string) (string, bool) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" || strings.Contains(trimmed, "```") {
		return "", false
	}
	if !codeModeHostCall.MatchString(trimmed) {
		return "", false
	}
	if !strings.HasSuffix(trimmed, ";") && !strings.HasSuffix(trimmed, "}") && !strings.HasSuffix(trimmed, ")") {
		return "", false
	}
	return trimmed, true
}

// GrammarBodyCall claims an unfenced grammar-tool body as a tool call.
//
// It runs only when the request declares a grammar tool and it consumes the
// whole of the text: a body is the entire answer, never a fragment inside
// prose. The bridge envelope names no tool, so it is attributed to the first
// declared grammar tool the caller still allows; under code mode that is
// `exec`, the only one taking a program.
func GrammarBodyCall(text string, tools []ToolDef, allowed func(string) bool) (ToolCall, bool) {
	names := GrammarToolNames(tools)
	if len(names) == 0 {
		return ToolCall{}, false
	}

	pick := ""
	for _, name := range names {
		if allowed == nil || allowed(name) {
			pick = name
			break
		}
	}
	if pick == "" {
		return ToolCall{}, false
	}

	body, ok := BridgeEnvelopeBody(text)
	if !ok {
		body, ok = CodeModeSourceCall(text)
	}
	if !ok {
		return ToolCall{}, false
	}

	// A custom tool carries its free-form body directly, the way the regular
	// custom tool path does; the `input` field of the emitted item is this
	// text. The bridge envelope exists only to get the body through M365's
	// JSON-only argument channel, so it is unwrapped here rather than passed on.
	return ToolCall{ID: nextToolCallID(), Name: pick, Arguments: json.RawMessage(body)}, true
}
