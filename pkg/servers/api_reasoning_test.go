package servers

import (
	"strings"
	"testing"

	"github.com/KilimcininKorOglu/M365Bridge/pkg/models"
)

func TestReasoningEffortValidation(t *testing.T) {
	if deliberate, err := reasoningEffortRequestsDeliberation(nil); err != nil || deliberate {
		t.Fatalf("absent reasoning block: deliberate=%v err=%v, want false and no error", deliberate, err)
	}

	// Codex sends the effort with varying case and spacing.
	for _, effort := range []string{"none", "minimal", "low", "MEDIUM", " high ", "xhigh", "max"} {
		if _, err := reasoningEffortRequestsDeliberation(&responsesReasoning{Effort: effort}); err != nil {
			t.Fatalf("effort %q rejected: %v", effort, err)
		}
	}

	err := reasoningEffortRequestsDeliberationError(t, "turbo")
	if !strings.Contains(err, "turbo") {
		t.Fatalf("error %q does not name the rejected value", err)
	}
	for _, name := range models.ReasoningEffortNames() {
		if !strings.Contains(err, name) {
			t.Fatalf("error %q does not list the accepted value %q", err, name)
		}
	}
}

func reasoningEffortRequestsDeliberationError(t *testing.T, effort string) string {
	t.Helper()
	_, err := reasoningEffortRequestsDeliberation(&responsesReasoning{Effort: effort})
	if err == nil {
		t.Fatalf("effort %q was accepted", effort)
	}
	return err.Error()
}

func TestReasoningEffortThreshold(t *testing.T) {
	cases := map[string]bool{
		"none":    false,
		"minimal": false,
		"low":     false,
		"medium":  true,
		"high":    true,
		"xhigh":   true,
		"max":     true,
	}
	for effort, want := range cases {
		got, err := reasoningEffortRequestsDeliberation(&responsesReasoning{Effort: effort})
		if err != nil {
			t.Fatalf("effort %q rejected: %v", effort, err)
		}
		if got != want {
			t.Fatalf("effort %q asks to deliberate = %v, want %v", effort, got, want)
		}
	}
}

func TestApplyReasoningEffortRoutesToTheVariant(t *testing.T) {
	base := models.LookupModel("gpt5.5")
	got := applyReasoningEffort("gpt5.5", base, true)
	if got.Tone != models.ModelRegistry["gpt5.5-reasoning"].Tone {
		t.Fatalf("tone = %q, want the reasoning variant", got.Tone)
	}
}

func TestApplyReasoningEffortLeavesModelsWithoutAVariant(t *testing.T) {
	// The tone is the only lever, so a model with no reasoning variant must be
	// left alone rather than silently swapped for an unrelated one.
	base := models.LookupModel("claude-opus")
	if got := applyReasoningEffort("claude-opus", base, true); got.Tone != base.Tone {
		t.Fatalf("model without a variant was rerouted to %q", got.Tone)
	}

	// A key that already names a reasoning variant must not grow a second suffix.
	variant := models.LookupModel("gpt5.5-reasoning")
	if got := applyReasoningEffort("gpt5.5-reasoning", variant, true); got.Tone != variant.Tone {
		t.Fatalf("reasoning variant was rerouted to %q", got.Tone)
	}

	// Low effort leaves the tone alone even when a variant exists.
	base = models.LookupModel("gpt5.5")
	if got := applyReasoningEffort("gpt5.5", base, false); got.Tone != base.Tone {
		t.Fatalf("low effort rerouted the model to %q", got.Tone)
	}
}
