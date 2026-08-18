package toolcalling

import "strings"

// M365 Copilot runs its own code interpreter, so when a caller declares a tool
// the backend can answer in two ways that both leave the agent loop with
// nothing to execute: it denies having tools at all, or it claims to have
// already run the work in its own sandbox. Neither produces a tool call, and
// neither reads as an error, so an agent client such as Claude Code or Codex
// stalls on a confident-sounding reply.
//
// Both are recognized from the reply text and answered with one re-ask.

// toolRefusalPatterns match a reply that denies the declared tools exist.
var toolRefusalPatterns = []string{
	"i don't have any tools",
	"i do not have any tools",
	"i don't have access to any tools",
	"i do not have access to any tools",
	"no tools are available",
	"not actually available",
	"not exposed in this",
	"aren't available in this",
	"are not available in this",
	"i'm unable to call tools",
	"i am unable to call tools",
	"tool calling is not supported",
	"tool invocation is not supported",
}

// sandboxHallucinationPatterns match a reply that claims the work already ran
// somewhere other than the caller's tools.
var sandboxHallucinationPatterns = []string{
	"i'll run that",
	"i will run that",
	"i'll execute",
	"i will execute",
	"let me run it",
	"running it now",
	"code interpreter",
	"python sandbox",
	"my sandbox",
	"/mnt/data",
	"linux container",
	"execution environment has changed",
	"in my environment",
}

// IsToolRefusal reports whether the reply denies that the caller's tools exist.
func IsToolRefusal(text string) bool {
	return matchesAny(text, toolRefusalPatterns)
}

// IsSandboxHallucination reports whether the reply claims to have run the work
// itself instead of calling one of the caller's tools.
func IsSandboxHallucination(text string) bool {
	return matchesAny(text, sandboxHallucinationPatterns)
}

func matchesAny(text string, patterns []string) bool {
	lowered := strings.ToLower(text)
	for _, pattern := range patterns {
		if strings.Contains(lowered, pattern) {
			return true
		}
	}
	return false
}

// BuildNativeToolBanNote constructs the corrective instruction sent when the
// backend answered a tool request without emitting a tool call. It states the
// two failure modes explicitly, because a generic "please use the tools" note
// leaves the model free to repeat the same claim.
func BuildNativeToolBanNote() string {
	return "RETRY: You answered without emitting a tool call. " +
		nativeToolBanInstruction + " " +
		"Re-emit the JSON envelope with the tool call that this request needs."
}

// nativeToolBanInstruction is shared by the simulated prompts and the re-ask,
// so the constraint is stated the same way before and after the failure.
//
// Its wording deliberately avoids every pattern above: the instruction travels
// in the request, a model that echoes it would otherwise trip the detector it
// was meant to prevent.
const nativeToolBanInstruction = "The tools declared in this request are real and the caller executes them, not you. " +
	"Never state that they are unavailable to you. " +
	"You have no built-in execution environment here, so never claim you already ran, " +
	"executed, or simulated the work yourself. " +
	"Emitting a tool call is the only way to act."
