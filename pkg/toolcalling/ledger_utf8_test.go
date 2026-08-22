package toolcalling

import (
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

// turkishFiller repeats a two-byte character, so every byte-sized cap lands in
// the middle of a character unless the cut looks for a boundary.
func turkishFiller(runes int) string {
	return strings.Repeat("ş", runes)
}

// A tool result is whatever the tool printed. The caps are byte counts, so a
// cut that ignores rune boundaries puts an invalid byte into the prompt the
// model reads.
func TestCompactResultKeepsEveryCharacterWhole(t *testing.T) {
	// Long enough to be compacted, and made of two-byte characters throughout.
	result := turkishFiller(maxEvidenceResult)

	compacted := compactResult(result)

	if !utf8.ValidString(compacted) {
		t.Fatalf("compacted result is not valid UTF-8: %q", compacted[:32])
	}
	if !strings.Contains(compacted, "truncated") {
		t.Fatalf("a result above the cap was not compacted: len=%d", len(compacted))
	}
	if strings.ContainsRune(compacted, utf8.RuneError) {
		t.Error("compaction left a replacement character in the result")
	}
}

// The reported byte count must match what the two cuts actually removed, or the
// model is told a different amount of the log is missing than really is.
func TestCompactResultReportsWhatItRemoved(t *testing.T) {
	result := turkishFiller(maxEvidenceResult)

	compacted := compactResult(result)

	_, rest, found := strings.Cut(compacted, "[truncated ")
	if !found {
		t.Fatalf("no truncation marker in %q", compacted[:32])
	}
	count, _, _ := strings.Cut(rest, " bytes]")
	removed, err := strconv.Atoi(count)
	if err != nil {
		t.Fatalf("cannot read the removed count %q: %v", count, err)
	}

	marker := "\n... [truncated " + count + " bytes] ...\n"
	kept := len(compacted) - len(marker)
	if want := len(result) - removed; kept != want {
		t.Errorf("marker claims %d bytes removed, leaving %d, but %d survived", removed, want, kept)
	}
}

func TestNormalizeFailureKeepsEveryCharacterWhole(t *testing.T) {
	// One leading ASCII byte puts the cap at an odd offset, which falls inside
	// a two-byte character rather than between two of them.
	signature := normalizeFailure("x" + turkishFiller(maxFailureSignature))

	if !utf8.ValidString(signature) {
		t.Fatalf("failure signature is not valid UTF-8: %q", signature)
	}
	if len(signature) > maxFailureSignature {
		t.Errorf("signature = %d bytes, above the cap of %d", len(signature), maxFailureSignature)
	}
}

func TestRuneStartHelpersLandOnBoundaries(t *testing.T) {
	// "şş" is four bytes: 0,1 then 2,3.
	s := "şş"

	if got := runeStartAtOrBefore(s, 1); got != 0 {
		t.Errorf("runeStartAtOrBefore(1) = %d, want 0", got)
	}
	if got := runeStartAtOrBefore(s, 2); got != 2 {
		t.Errorf("runeStartAtOrBefore(2) = %d, want 2", got)
	}
	if got := runeStartAtOrBefore(s, len(s)); got != len(s) {
		t.Errorf("runeStartAtOrBefore(len) = %d, want %d", got, len(s))
	}
	if got := runeStartAtOrAfter(s, 1); got != 2 {
		t.Errorf("runeStartAtOrAfter(1) = %d, want 2", got)
	}
	if got := runeStartAtOrAfter(s, 3); got != len(s) {
		t.Errorf("runeStartAtOrAfter(3) = %d, want %d", got, len(s))
	}
	if got := runeStartAtOrAfter(s, -1); got != 0 {
		t.Errorf("runeStartAtOrAfter(-1) = %d, want 0", got)
	}
}
