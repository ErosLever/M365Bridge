package servers

import (
	"strings"
	"testing"

	"github.com/KilimcininKorOglu/M365Bridge/pkg/models"
)

// The listing is what a user consults to pick a -model value, so a registry
// entry missing from it is a model nobody can find.
func TestPrintModelsCoversTheWholeRegistry(t *testing.T) {
	var out strings.Builder
	PrintModels(&out)
	text := out.String()

	for key, cfg := range models.ModelRegistry {
		if !strings.Contains(text, key) {
			t.Errorf("registry key %q is missing from the listing", key)
		}
		if !strings.Contains(text, cfg.OpenAIID) {
			t.Errorf("%q: api id %q is missing from the listing", key, cfg.OpenAIID)
		}
		if !strings.Contains(text, cfg.Tone) {
			t.Errorf("%q: tone %q is missing from the listing", key, cfg.Tone)
		}
	}
}

// The CLI name and the advertised id differ for most entries, and sending the
// wrong one to the HTTP API now returns 404 model_not_found. The listing has to
// show both so the difference is visible rather than surprising.
func TestPrintModelsDistinguishesTheCLINameFromTheAPIID(t *testing.T) {
	var out strings.Builder
	PrintModels(&out)
	text := out.String()

	if !strings.Contains(text, "api id") {
		t.Fatal("the listing does not label the api id column")
	}

	cfg, ok := models.ModelRegistry["gpt5.5-reasoning"]
	if !ok {
		t.Fatal("gpt5.5-reasoning left the registry; pick another entry for this test")
	}
	if cfg.OpenAIID == "gpt5.5-reasoning" {
		t.Skip("the CLI name and the api id are identical, so this test proves nothing")
	}
	for _, want := range []string{"gpt5.5-reasoning", cfg.OpenAIID} {
		if !strings.Contains(text, want) {
			t.Errorf("the listing does not carry %q", want)
		}
	}
}
