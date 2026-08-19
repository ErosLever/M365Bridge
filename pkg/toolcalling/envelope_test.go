package toolcalling

import (
	"strings"
	"testing"
)

func TestStripTransportEnvelopeRemovesTheFencedBlock(t *testing.T) {
	// A truncated envelope is exactly the case that reaches this path: the
	// parser found nothing usable, so the raw block would otherwise be shown to
	// the user as the answer.
	raw := "```json\n{\"choices\":[{\"message\":{\"role\":\"assistant\",\"tool_calls\":[{\"id\""

	got, withheld := StripTransportEnvelope(raw)
	if !withheld {
		t.Fatal("a transport envelope was not recognized")
	}
	if strings.Contains(got, "tool_calls") {
		t.Fatalf("envelope survived into %q", got)
	}
}

func TestStripTransportEnvelopeKeepsTheAnswerAroundIt(t *testing.T) {
	raw := "Here is what I found.\n```json\n{\"choices\":[]}\n```\nLet me know if that helps."

	got, withheld := StripTransportEnvelope(raw)
	if !withheld {
		t.Fatal("the envelope was not withheld")
	}
	if !strings.Contains(got, "Here is what I found.") || !strings.Contains(got, "Let me know if that helps.") {
		t.Fatalf("prose around the envelope was lost: %q", got)
	}
	if strings.Contains(got, "choices") {
		t.Fatalf("envelope survived into %q", got)
	}
}

func TestStripTransportEnvelopeLeavesOrdinaryAnswersAlone(t *testing.T) {
	// A non-JSON fence is real content the user asked for.
	raw := "Run this:\n```bash\necho hello\n```\nThat prints a greeting."

	got, withheld := StripTransportEnvelope(raw)
	if withheld {
		t.Fatalf("an ordinary answer was treated as transport: %q", got)
	}
	if got != raw {
		t.Fatalf("answer changed to %q", got)
	}
}

func TestWithholdTransportEnvelopeSubstitutesANotice(t *testing.T) {
	// An answer that was nothing but transport leaves an empty message, which
	// downstream reads as an empty upstream response.
	raw := "```json\n{\"choices\":[{\"message\""

	if got := WithholdTransportEnvelope(raw); got != WithheldEnvelopeNotice {
		t.Fatalf("got %q, want the withheld notice", got)
	}
}

func TestWithholdTransportEnvelopePassesCleanTextThrough(t *testing.T) {
	raw := "The file has 12 lines."
	if got := WithholdTransportEnvelope(raw); got != raw {
		t.Fatalf("clean text changed to %q", got)
	}
}
