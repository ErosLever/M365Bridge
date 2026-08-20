package client

import (
	"strings"
	"testing"
)

// The delimiters are written as escapes in the source because they are
// invisible. These build the same runs a turn actually carries.
const (
	start = "\ue200"
	sep   = "\ue202"
	stop  = "\ue201"
)

func TestStripCitationsRemovesAWholeRun(t *testing.T) {
	// Observed on a web-grounded answer: the run renders as the word "cite"
	// fused to a reference id, because the delimiters have no glyph.
	input := "Hava güneşli. " + start + "cite" + sep + "turn1search1" + stop + "\n\n- Durum: Güneşli"
	want := "Hava güneşli. \n\n- Durum: Güneşli"

	if got := stripCitations(input); got != want {
		t.Errorf("stripCitations = %q, want %q", got, want)
	}
}

func TestStripCitationsRemovesAMultiReferenceRun(t *testing.T) {
	input := "Yağmur beklenmiyor. " + start + "cite" + sep + "turn1search1" + sep + "turn1search7" + stop + " Devam."
	want := "Yağmur beklenmiyor.  Devam."

	if got := stripCitations(input); got != want {
		t.Errorf("stripCitations = %q, want %q", got, want)
	}
}

func TestStripCitationsRemovesEveryRun(t *testing.T) {
	input := "A" + start + "cite" + sep + "t1" + stop + "B" + start + "cite" + sep + "t2" + stop + "C"
	if got := stripCitations(input); got != "ABC" {
		t.Errorf("stripCitations = %q, want ABC", got)
	}
}

// A resolved markdown link arrives on the other channel and is real answer
// content, so it must survive untouched.
func TestStripCitationsKeepsAResolvedLink(t *testing.T) {
	input := "Nem %23. [1](https://a.msn.com/54/EN-US/ct39.8965,32.862?pageocid=x)"
	if got := stripCitations(input); got != input {
		t.Errorf("stripCitations altered text that carried no marker: %q", got)
	}
}

func TestStripCitationsLeavesOrdinaryTextAlone(t *testing.T) {
	for _, input := range []string{"", "plain answer", "citation is a normal word here"} {
		if got := stripCitations(input); got != input {
			t.Errorf("stripCitations(%q) = %q", input, got)
		}
	}
}

// A run that never closes is a truncated marker, not answer text.
func TestStripCitationsDropsAnUnterminatedRun(t *testing.T) {
	input := "Sıcaklık 28 derece. " + start + "cite" + sep + "turn1sea"
	if got := stripCitations(input); got != "Sıcaklık 28 derece. " {
		t.Errorf("stripCitations = %q", got)
	}
}

// The stream splits wherever the transport splits, so a run can arrive across
// several deltas. Emitting the first half and repairing later is impossible:
// a delta on the wire cannot be retracted.
func TestCitationFilterHoldsARunSplitAcrossDeltas(t *testing.T) {
	chunks := []string{
		"Hava güneşli. " + start + "cit",
		"e" + sep + "turn1sea",
		"rch1" + stop + " Devam ediyor.",
	}
	var out strings.Builder
	var filter citationFilter
	for _, chunk := range chunks {
		out.WriteString(filter.push(chunk))
	}
	if held := filter.flush(); held != "" {
		t.Errorf("the filter still held %q after a closed run", held)
	}

	want := "Hava güneşli.  Devam ediyor."
	if out.String() != want {
		t.Errorf("streamed text = %q, want %q", out.String(), want)
	}
}

// Nothing may be emitted between the start delimiter and the end delimiter,
// because that is exactly the text a client would render as noise.
func TestCitationFilterEmitsNothingWhileARunIsOpen(t *testing.T) {
	var filter citationFilter
	if got := filter.push("Metin " + start + "cite" + sep); got != "Metin " {
		t.Fatalf("first push = %q, want %q", got, "Metin ")
	}
	if got := filter.push("turn1search1"); got != "" {
		t.Errorf("an open run leaked %q", got)
	}
	if got := filter.push(stop + " son"); got != " son" {
		t.Errorf("after close = %q, want %q", got, " son")
	}
}

func TestCitationFilterDropsAnUnterminatedRunOnFlush(t *testing.T) {
	var filter citationFilter
	if got := filter.push("Cevap. " + start + "cite" + sep + "turn1"); got != "Cevap. " {
		t.Fatalf("push = %q", got)
	}
	held := filter.flush()
	if held == "" {
		t.Fatal("flush reported nothing, so a dropped marker would be invisible in the log")
	}
	if !strings.HasPrefix(held, start) {
		t.Errorf("flush held %q, which does not start at the marker", held)
	}
	if got := filter.flush(); got != "" {
		t.Errorf("flush is not idempotent: second call returned %q", got)
	}
}

// The filter runs on every streamed delta, so text with no marker at all must
// pass through byte for byte.
func TestCitationFilterPassesCleanTextThrough(t *testing.T) {
	var filter citationFilter
	var out strings.Builder
	for _, chunk := range []string{"Bugün ", "Ankara'da ", "hava güneşli."} {
		out.WriteString(filter.push(chunk))
	}
	if out.String() != "Bugün Ankara'da hava güneşli." {
		t.Errorf("clean text was altered: %q", out.String())
	}
	if filter.flush() != "" {
		t.Error("clean text left something held")
	}
}
