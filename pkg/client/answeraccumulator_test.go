package client

import (
	"encoding/json"
	"strings"
	"testing"
)

// The markdown the gateway emits for a generated image, as
// extractImageGenerationMarkdown writes it.
const generatedImageMarkdown = "\n\n![image](https://designerapp.officeapps.live.com/i.png?fileToken=x)\n\n"

// imageProgressMessage decodes a Progress message in the shape the backend
// sends once a generated image is ready.
func imageProgressMessage(t *testing.T, url string) map[string]any {
	t.Helper()
	raw := `{"contentOrigin":"ImageGeneration",
		"contentGenerationProgressList":[{"ImageReferenceUrls":["` + url + `"]}]}`
	var msg map[string]any
	if err := json.Unmarshal([]byte(raw), &msg); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	return msg
}

// The measured failure. An image went out first, and the answer's first
// snapshot was compared against a baseline that already held the image
// markdown. The snapshot diverged from it, so it was dropped as a re-encoding,
// and the opening words never reached the client: the reader saw the answer
// start mid-word.
func TestEmitGeneratedImageKeepsTheAnswersFirstSnapshot(t *testing.T) {
	const url = "https://designerapp.officeapps.live.com/i.png?fileToken=x"
	var acc answerAccumulator
	var emitted []string
	emit := func(chunk StreamChunk) bool {
		emitted = append(emitted, chunk.Text)
		return true
	}

	if !acc.emitGeneratedImage(imageProgressMessage(t, url), map[string]bool{}, emit) {
		t.Fatal("emitGeneratedImage stopped a stream the caller may continue")
	}
	if len(emitted) != 1 || !strings.Contains(emitted[0], url) {
		t.Fatalf("emitted = %q, want one chunk carrying the image link", emitted)
	}
	if acc.baseline() != "" {
		t.Fatalf("baseline = %q, want empty; the image link is not answer text", acc.baseline())
	}
	if acc.emittedBytes() != len(emitted[0]) {
		t.Fatalf("emittedBytes = %d, want %d", acc.emittedBytes(), len(emitted[0]))
	}

	const answer = "Buyur, turuncu kedi hazır!"
	chunk, advanced := snapshotDelta(acc.baseline(), answer)
	if !advanced || chunk != answer {
		t.Fatalf("chunk=%q advanced=%v; the answer's first snapshot was dropped", chunk, advanced)
	}
}

// A Progress message without an image must emit nothing and let the stream go
// on, because most Progress messages carry no image at all.
func TestEmitGeneratedImageIgnoresAMessageWithoutAnImage(t *testing.T) {
	var acc answerAccumulator
	emit := func(StreamChunk) bool {
		t.Error("a message without an image produced a chunk")
		return true
	}

	var msg map[string]any
	if err := json.Unmarshal([]byte(`{"contentOrigin":"ImageGeneration"}`), &msg); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	if !acc.emitGeneratedImage(msg, map[string]bool{}, emit) {
		t.Fatal("a message without an image stopped the stream")
	}
	if acc.emittedBytes() != 0 {
		t.Fatalf("emittedBytes = %d, want 0", acc.emittedBytes())
	}
}

// The same comparison with the image in the baseline, which is what the code
// did before. It states the cost of putting injected text there.
func TestSnapshotIsLostWhenInjectedTextEntersTheBaseline(t *testing.T) {
	chunk, advanced := snapshotDelta(generatedImageMarkdown, "Buyur, turuncu kedi hazır!")
	if advanced || chunk != "" {
		t.Fatal("fixture no longer reproduces the failure this rule guards against")
	}
}

func TestAccumulatorTracksAnswerAndInjectedTextTogether(t *testing.T) {
	var acc answerAccumulator
	acc.appendAnswer("Buy")
	acc.inject(len(generatedImageMarkdown))
	acc.appendAnswer("ur")

	if acc.baseline() != "Buyur" {
		t.Fatalf("baseline = %q, want %q", acc.baseline(), "Buyur")
	}
	if want := len("Buyur") + len(generatedImageMarkdown); acc.emittedBytes() != want {
		t.Fatalf("emittedBytes = %d, want %d", acc.emittedBytes(), want)
	}

	// A snapshot restates the answer alone, so it replaces the answer and
	// leaves the injected count where it is.
	acc.replaceAnswer("Buyur, turuncu kedi hazır!")
	if acc.baseline() != "Buyur, turuncu kedi hazır!" {
		t.Fatalf("baseline after a snapshot = %q", acc.baseline())
	}
	if want := len("Buyur, turuncu kedi hazır!") + len(generatedImageMarkdown); acc.emittedBytes() != want {
		t.Fatalf("emittedBytes after a snapshot = %d, want %d", acc.emittedBytes(), want)
	}
}
