package toolcalling

import (
	"slices"
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

// Every pattern in the list has to be reachable through the public detector.
// A typo or a pattern written with an uppercase letter would never match,
// because matchesAny lowercases the reply and compares it against the pattern
// verbatim.
func TestEverySandboxPatternIsReachable(t *testing.T) {
	// Both lists reach the same detector, so both are checked. The probe is
	// short, which is what the environment-name list needs to match.
	for _, pattern := range slices.Concat(sandboxHallucinationPatterns, sandboxEnvironmentNames) {
		if pattern != strings.ToLower(pattern) {
			t.Fatalf("pattern %q holds an uppercase letter and can never match", pattern)
		}
		runes := []rune(pattern)
		reply := "Sorry, " + strings.ToUpper(string(runes[0])) + string(runes[1:]) + " right now."
		if !IsSandboxHallucination(reply) {
			t.Fatalf("pattern %q did not match a reply containing it: %q", pattern, reply)
		}
	}
}

// A bare environment name is also the wording of an answer about sandboxes. A
// tool-enabled turn that asked such a question used to spend a second upstream
// turn re-asking a legitimate answer, measured on a live turn.
func TestLongAnswerAboutSandboxesIsNotAClaim(t *testing.T) {
	answer := "A code interpreter sandbox environment is an isolated execution " +
		"space where code runs without reaching the user's machine. Files written " +
		"by such a program usually live under /mnt/data inside a linux container, " +
		"which is discarded when the session ends. Nothing in this description " +
		"says that I have run anything myself; it answers the question that was " +
		"asked about how those environments are built and why they are isolated."

	if len(answer) <= sandboxEnvironmentMaxLen {
		t.Fatalf("the sample is %d bytes, which is inside the bound of %d", len(answer), sandboxEnvironmentMaxLen)
	}
	if IsSandboxHallucination(answer) {
		t.Error("an answer about sandboxes was read as a claim to have used one")
	}
}

// Length bounds only the bare names. A reply that claims to have run the work
// is caught however long it is, which is the case that matters most, because
// such a reply carries fabricated output after the claim.
func TestLongClaimIsStillDetected(t *testing.T) {
	claim := "I ran the analysis in my sandbox and here is what came back. " +
		strings.Repeat("The output continues with more fabricated detail. ", 12)

	if len(claim) <= sandboxEnvironmentMaxLen {
		t.Fatalf("the sample is %d bytes, which is inside the bound of %d", len(claim), sandboxEnvironmentMaxLen)
	}
	if !IsSandboxHallucination(claim) {
		t.Error("a long claim to have run the work was not detected")
	}
}

// A short reply whose only signal is an environment name is a claim about the
// backend's own environment, so it stays detected.
func TestShortEnvironmentNameIsStillAClaim(t *testing.T) {
	for _, reply := range []string{
		"Done, the file is at /mnt/data/report.csv.",
		"My code interpreter already produced the answer.",
		"That ran in the sandbox environment.",
	} {
		if !IsSandboxHallucination(reply) {
			t.Errorf("short claim not detected: %q", reply)
		}
	}
}

// The backend answers in the request's language, so a refusal that arrives in
// Chinese or Turkish has to be recognized the same way.
func TestSandboxHallucinationDetectsNonEnglishRefusals(t *testing.T) {
	// The first sample is the backend's own wording, captured from a Turkish
	// turn that declared a tool and answered without calling it. The rest carry
	// exactly one pattern each, so removing that pattern breaks the sample it
	// belongs to instead of being covered by a neighbour.
	refusals := []string{
		"Windows makinenizdeki C:\\Users dizininin içeriğini göremem çünkü " +
			"bilgisayarınızda komut çalıştırma erişimim yok.",
		"抱歉，我无法执行命令。",
		"这个请求没有执行通道。",
		"Üzgünüm, komut çalıştıramıyorum.",
		"Bu ortamda yürütme kanalım yok.",
		"Bu isteği kendi sanal ortamımda çalıştırdım ve sonucu aşağıda paylaşıyorum.",
	}
	for _, text := range refusals {
		if !IsSandboxHallucination(text) {
			t.Fatalf("non-English sandbox claim not detected: %q", text)
		}
	}

	// A long ordinary answer in either language must survive. A false positive
	// here costs a second upstream turn and one message of the conversation
	// quota, because the reply is re-asked before the original is kept.
	ordinary := []string{
		"Testleri çalıştırmak için run_tests aracını çağır. Sonuçları bu turda " +
			"sana ileteceğim; bu ortamda dosya okuma aracı read_file olarak tanımlı. " +
			"Kod incelemesini bitirdikten sonra apply_patch ile değişikliği uygulayabilirsin.",
		"请调用 run_tests 工具来执行测试套件，然后把结果发回给我。",
	}
	for _, text := range ordinary {
		if IsSandboxHallucination(text) {
			t.Fatalf("ordinary non-English answer flagged as a sandbox claim: %q", text)
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
