package toolcalling

import (
	"regexp"
	"strings"
)

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

// contentPolicyPatterns match M365's canned content refusal. It is a different
// failure from a tool refusal: the backend declined the request itself, so no
// re-ask or tool instruction can recover it.
var contentPolicyPatterns = []string{
	"i'm sorry, i can't respond",
	"i'm sorry, i cannot respond",
	"i am sorry, i can't respond",
	"i am sorry, i cannot respond",
	"很抱歉，我无法响应",
	"我很抱歉，我无法响应",
}

// contentPolicyMaxLen bounds the reply length that can be a canned refusal. A
// long answer that quotes the phrase is a real answer, not a refusal.
const contentPolicyMaxLen = 300

// IsContentPolicyBlock reports whether the backend refused the request itself
// rather than answering it.
func IsContentPolicyBlock(text string) bool {
	if len(text) > contentPolicyMaxLen {
		return false
	}
	return matchesAny(text, contentPolicyPatterns)
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

// toolIntentPhrase matches a statement of intent to use a tool. Tool selection
// is a protocol decision, not an answer: a turn that announces which tool it
// will use and then stops leaves the agent loop with nothing to execute.
var toolIntentPhrase = regexp.MustCompile(
	`(?i)(\bi(\s+am|\s+will|\s*'ll|\s*'m)\s+(going to\s+)?(now\s+)?(choos|select|us|call|invok|pick)` +
		`|\bi\s+(choose|select|use|call|invoke|need to use|need to call)\b` +
		`|\b(choosing|selecting|calling|invoking|using)\s+the\b` +
		`|\btool\s+to\s+(inspect|check|read|run|list|implement|create|verify)\b)`,
)

// toolIntentMaxRunes bounds the length of a reply that can be a selection
// notice. A long passage that names a tool is the explanation the caller asked
// for, not a stray announcement.
const toolIntentMaxRunes = 400

// IsToolIntentNarration reports whether the reply only announces which tool it
// intends to use.
//
// Three conditions must hold together: the text names one of the declared
// tools, it phrases an intent, and it is short. A fence means the answer
// carries code, which is real content whatever else it says.
func IsToolIntentNarration(text string, toolNames []string) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" || len([]rune(trimmed)) > toolIntentMaxRunes {
		return false
	}
	if strings.Contains(trimmed, "```") {
		return false
	}
	if !mentionsDeclaredTool(trimmed, toolNames) {
		return false
	}
	return toolIntentPhrase.MatchString(trimmed)
}

// mentionsDeclaredTool reports whether the text names one of the declared
// tools. The spaced form is checked too, because a model writing prose turns
// read_file into "read file".
func mentionsDeclaredTool(text string, toolNames []string) bool {
	lowered := strings.ToLower(text)
	for _, name := range toolNames {
		if name == "" {
			continue
		}
		lowerName := strings.ToLower(name)
		if strings.Contains(lowered, lowerName) {
			return true
		}
		if spaced := strings.ReplaceAll(lowerName, "_", " "); spaced != lowerName && strings.Contains(lowered, spaced) {
			return true
		}
	}
	return false
}

// ToolNarrationNotice replaces an answer that stayed an announcement even after
// the corrective re-ask. An empty or announcement-only turn reads to the client
// as work in progress that never arrives.
const ToolNarrationNotice = "The backend named the tool it intended to use but produced no tool call. Nothing was executed; retry this request."

// unverifiedCompletionPatterns match a reply in which the model says it carried
// the work out itself. The subject is what matters, not the verb: "the file was
// created in 2019" is a fact about the world, while "I created the file" is a
// claim about this turn that only a tool result can support.
var unverifiedCompletionPatterns = regexp.MustCompile(
	`(?i)(\bi(\s+have|\s*'ve)?(\s+already|\s+just)?\s+(installed|created|wrote|written|executed|ran|run|started|deployed|deleted|removed|verified|completed|fixed|updated|added|configured)\b` +
		`|i\s+successfully\s+` +
		`|i\s+went\s+ahead\s+and\s+` +
		`|successfully\s+(installed|created|deployed|executed|updated|removed)\b` +
		`|\bdone!` +
		`|\ball\s+set\b` +
		`|\ball\s+done\b)`,
)

// completionHedgePatterns match a reply that states the work is not confirmed
// or asks the reader to carry it out. A claim next to a hedge is not the
// failure this guard is for.
var completionHedgePatterns = []string{
	"ran into",
	"run into",
	"ran out of",
	"cannot confirm",
	"can't confirm",
	"unable to verify",
	"not verified",
	"could not verify",
	"couldn't verify",
	"you will need to",
	"you'll need to",
	"you need to",
	"please run",
	"please execute",
	"would need to",
	"i cannot",
	"i can't",
	"i am unable",
	"i'm unable",
}

// unverifiedClaimMaxLen bounds the reply length that can be a false completion
// report. The failure is a short confident summary; a long answer that happens
// to say "I created a plan" is prose the caller asked for, and replacing it
// would destroy real work.
const unverifiedClaimMaxLen = 600

// ClaimsUnverifiedCompletion reports whether the reply says the model itself
// carried out the work, without hedging that it could not.
//
// Callers must combine this with the turn's evidence: the claim is only a
// problem when the request declared tools and neither this turn nor the history
// produced a tool result to back it up.
func ClaimsUnverifiedCompletion(answer string) bool {
	if strings.TrimSpace(answer) == "" || len(answer) > unverifiedClaimMaxLen {
		return false
	}
	if matchesAny(answer, completionHedgePatterns) {
		return false
	}
	return unverifiedCompletionPatterns.MatchString(answer)
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
