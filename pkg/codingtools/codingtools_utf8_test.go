package codingtools

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// Every character here is two bytes, so an odd byte cap falls inside one of
// them unless the cut looks for a rune boundary.
const twoByteRune = "ş"

func TestBoundKeepsEveryCharacterWhole(t *testing.T) {
	value := strings.Repeat(twoByteRune, 10)

	// 15 bytes cuts the eighth character in half.
	got, truncated, err := bound(value, 15)
	if err != nil {
		t.Fatalf("bound: %v", err)
	}
	if !truncated {
		t.Fatal("a value above the limit was not reported as truncated")
	}
	if !utf8.ValidString(got) {
		t.Fatalf("bound produced invalid UTF-8: %q", got)
	}
	if len(got) > 15 {
		t.Errorf("bound returned %d bytes, above the limit of 15", len(got))
	}
}

func TestBoundLeavesAValueUnderTheLimitAlone(t *testing.T) {
	value := strings.Repeat(twoByteRune, 3)

	got, truncated, err := bound(value, 100)
	if err != nil {
		t.Fatalf("bound: %v", err)
	}
	if truncated || got != value {
		t.Errorf("bound = %q truncated=%v, want the value unchanged", got, truncated)
	}
}

// A command writes its output in chunks, and the limit can fall anywhere in
// one of them.
func TestLimitedBufferKeepsEveryCharacterWhole(t *testing.T) {
	buffer := &limitedBuffer{limit: 15}

	written, err := buffer.Write([]byte(strings.Repeat(twoByteRune, 10)))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	// The caller is told the whole chunk was consumed, so it does not retry.
	if written != 20 {
		t.Errorf("Write reported %d bytes, want the whole chunk of 20", written)
	}
	if !buffer.truncated {
		t.Error("a chunk above the limit was not reported as truncated")
	}
	if got := buffer.String(); !utf8.ValidString(got) {
		t.Fatalf("buffer holds invalid UTF-8: %q", got)
	}
	if got := buffer.String(); len(got) > 15 {
		t.Errorf("buffer holds %d bytes, above the limit of 15", len(got))
	}
}

func TestLimitedBufferKeepsWritingUntilTheLimit(t *testing.T) {
	buffer := &limitedBuffer{limit: 100}

	for range 3 {
		if _, err := buffer.Write([]byte(strings.Repeat(twoByteRune, 4))); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}

	if buffer.truncated {
		t.Error("a total below the limit was reported as truncated")
	}
	if got := buffer.String(); got != strings.Repeat(twoByteRune, 12) {
		t.Errorf("buffer = %q", got)
	}
}
