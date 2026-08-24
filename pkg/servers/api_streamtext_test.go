package servers

import (
	"strings"
	"testing"
)

// The streaming handlers read the accumulated answer mid-loop, for the
// max_tokens check and the Anthropic stop-sequence check, then keep writing.
// This guards the property that makes strings.Builder safe there: a string
// taken from the builder must not change when more text is appended.
func TestBuilderStringSurvivesLaterWrites(t *testing.T) {
	var b strings.Builder
	chunks := []string{"Bugün ", "web aramasıyla ", "kontrol ettim"}

	b.WriteString(chunks[0])
	early := b.String()
	b.WriteString(chunks[1])
	middle := b.String()
	b.WriteString(chunks[2])

	if early != chunks[0] {
		t.Fatalf("early snapshot changed to %q", early)
	}
	if middle != chunks[0]+chunks[1] {
		t.Fatalf("middle snapshot changed to %q", middle)
	}
	if got := b.String(); got != strings.Join(chunks, "") {
		t.Fatalf("final text = %q", got)
	}
}

func TestBuilderMatchesConcatenationOverManyChunks(t *testing.T) {
	// A long answer arrives as hundreds of chunks; the replacement must
	// produce the exact same text the old `fullText += chunk.Text` did.
	var b strings.Builder
	concatenated := ""
	for i := range 500 {
		chunk := string(rune('a'+i%26)) + " çok uzun bir yanıt parçası\n"
		b.WriteString(chunk)
		concatenated += chunk
	}
	if b.String() != concatenated {
		t.Fatal("builder text diverged from concatenation")
	}
	if countTokens(b.String()) != countTokens(concatenated) {
		t.Fatal("token count diverged between builder and concatenation")
	}
}
