package textcut

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// Every character here is two bytes, so an odd byte limit falls inside one of
// them rather than between two.
const twoByte = "ş"

func TestTruncateKeepsEveryCharacterWhole(t *testing.T) {
	value := strings.Repeat(twoByte, 10)

	got := Truncate(value, 15)

	if !utf8.ValidString(got) {
		t.Fatalf("Truncate produced invalid UTF-8: %q", got)
	}
	if len(got) != 14 {
		t.Errorf("len = %d, want 14 (the last whole character before 15)", len(got))
	}
}

func TestTruncateLeavesAValueUnderTheLimitAlone(t *testing.T) {
	value := strings.Repeat(twoByte, 3)

	if got := Truncate(value, 100); got != value {
		t.Errorf("Truncate = %q, want the value unchanged", got)
	}
	if got := Truncate(value, len(value)); got != value {
		t.Errorf("Truncate at the exact length = %q, want the value unchanged", got)
	}
}

func TestTruncateHandlesANonPositiveLimit(t *testing.T) {
	if got := Truncate(twoByte, 0); got != "" {
		t.Errorf("Truncate(0) = %q, want empty", got)
	}
	if got := Truncate(twoByte, -5); got != "" {
		t.Errorf("Truncate(-5) = %q, want empty", got)
	}
	// A limit inside the first character leaves nothing whole to keep.
	if got := Truncate(twoByte, 1); got != "" {
		t.Errorf("Truncate(1) = %q, want empty", got)
	}
}

func TestTruncateWorksOnBytes(t *testing.T) {
	got := Truncate([]byte(strings.Repeat(twoByte, 4)), 5)

	if !utf8.Valid(got) {
		t.Fatalf("Truncate produced invalid UTF-8: %q", got)
	}
	if len(got) != 4 {
		t.Errorf("len = %d, want 4", len(got))
	}
}

func TestStartAtOrBefore(t *testing.T) {
	// "şş" is four bytes: a character at 0-1 and another at 2-3.
	const s = twoByte + twoByte

	for _, c := range []struct {
		in, want int
	}{
		{0, 0}, {1, 0}, {2, 2}, {3, 2}, {4, 4}, {99, 4}, {-1, 0},
	} {
		if got := StartAtOrBefore(s, c.in); got != c.want {
			t.Errorf("StartAtOrBefore(%d) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestStartAtOrAfter(t *testing.T) {
	const s = twoByte + twoByte

	for _, c := range []struct {
		in, want int
	}{
		{0, 0}, {1, 2}, {2, 2}, {3, 4}, {4, 4}, {99, 99}, {-1, 0},
	} {
		if got := StartAtOrAfter(s, c.in); got != c.want {
			t.Errorf("StartAtOrAfter(%d) = %d, want %d", c.in, got, c.want)
		}
	}
}

// ASCII has no multi-byte character, so every index is already a boundary.
func TestASCIIIsCutExactlyAtTheLimit(t *testing.T) {
	if got := Truncate("abcdef", 3); got != "abc" {
		t.Errorf("Truncate = %q, want abc", got)
	}
}
