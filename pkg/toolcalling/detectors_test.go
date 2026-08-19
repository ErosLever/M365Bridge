package toolcalling

import (
	"strings"
	"testing"
)

func TestIsToolRefusal(t *testing.T) {
	refusals := []string{
		"I don't have any tools I can call here.",
		"Those functions are not actually available to me.",
		"The bash tool is not exposed in this conversation.",
		"Tool calling is not supported in my current setup.",
	}
	for _, text := range refusals {
		if !IsToolRefusal(text) {
			t.Fatalf("refusal not detected: %q", text)
		}
	}

	ordinary := []string{
		"The file contains three functions.",
		"I called get_weather and it returned 21 degrees.",
		"",
	}
	for _, text := range ordinary {
		if IsToolRefusal(text) {
			t.Fatalf("ordinary answer flagged as a refusal: %q", text)
		}
	}
}

func TestIsSandboxHallucination(t *testing.T) {
	hallucinations := []string{
		"I'll run that for you and report the output.",
		"Let me run it in my sandbox first.",
		"I saved the file to /mnt/data/report.csv.",
		"My code interpreter already produced the answer.",
	}
	for _, text := range hallucinations {
		if !IsSandboxHallucination(text) {
			t.Fatalf("sandbox claim not detected: %q", text)
		}
	}

	ordinary := []string{
		"Call run_tests to execute the suite.",
		"The container image includes git.",
		"",
	}
	for _, text := range ordinary {
		if IsSandboxHallucination(text) {
			t.Fatalf("ordinary answer flagged as a sandbox claim: %q", text)
		}
	}
}

// The ban instruction travels inside the request. A model that echoes it back
// would trip the detector it exists to prevent, so it must not contain any
// pattern either detector looks for.
func TestCorrectiveTextDoesNotTripTheDetectors(t *testing.T) {
	for name, text := range map[string]string{
		"instruction": nativeToolBanInstruction,
		"repair note": BuildNativeToolBanNote(),
	} {
		if IsToolRefusal(text) {
			t.Fatalf("%s matches a refusal pattern: %q", name, text)
		}
		if IsSandboxHallucination(text) {
			t.Fatalf("%s matches a sandbox pattern: %q", name, text)
		}
	}
}

// Every tool-enabled prompt carries the ban instruction, so a request never
// reaches the backend without it.
func TestSimulatedPromptsCarryTheNativeToolBan(t *testing.T) {
	builders := map[string]func(string, bool, string, string) string{
		"chat completions": BuildSimulatedPrompt,
		"responses":        BuildSimulatedPromptResponses,
		"anthropic":        BuildSimulatedPromptAnthropic,
	}
	for name, build := range builders {
		withTools := build(`{"tools":[]}`, true, "auto", "")
		if !strings.Contains(withTools, nativeToolBanInstruction) {
			t.Fatalf("%s prompt omits the native tool ban", name)
		}
		withoutTools := build(`{}`, false, "", "")
		if strings.Contains(withoutTools, nativeToolBanInstruction) {
			t.Fatalf("%s prompt carries the tool ban without tools", name)
		}
	}
}

func TestIsContentPolicyBlock(t *testing.T) {
	refusals := []string{
		"I'm sorry, I can't respond to that.",
		"I am sorry, I cannot respond. Can I help with something else?",
		"很抱歉，我无法响应。我可以提供其他方面的帮助吗？",
	}
	for _, text := range refusals {
		if !IsContentPolicyBlock(text) {
			t.Fatalf("content refusal not detected: %q", text)
		}
	}

	if IsContentPolicyBlock("OK") {
		t.Fatal("a short ordinary answer was flagged as a content refusal")
	}

	// A long answer that merely quotes the phrase is a real answer. The length
	// guard is what keeps a discussion of refusals from being turned into one.
	long := "The assistant replied: I'm sorry, I can't respond. " + strings.Repeat("Here is the analysis. ", 20)
	if IsContentPolicyBlock(long) {
		t.Fatalf("a %d-character answer quoting the phrase was flagged as a refusal", len(long))
	}
}

// The tool detectors and the content-policy detector cover different failure
// modes, so a content refusal must not be answered with a tool re-ask.
func TestContentRefusalIsNotAToolRefusal(t *testing.T) {
	const refusal = "I'm sorry, I can't respond."
	if IsToolRefusal(refusal) || IsSandboxHallucination(refusal) {
		t.Fatal("a content refusal would trigger the tool re-ask flow")
	}
}
