package servers

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestCutAtStopSequenceRemovesTheSequenceAndWhatFollows(t *testing.T) {
	got, matched := cutAtStopSequence("answer\n\nHuman: next question", []string{"\n\nHuman:"})

	if matched != "\n\nHuman:" {
		t.Fatalf("matched = %q, want the sequence that fired", matched)
	}
	if got != "answer" {
		t.Errorf("text = %q, want the text before the sequence", got)
	}
}

// With several sequences the completion ends at whichever arrives first, not
// at whichever was listed first.
func TestCutAtStopSequenceUsesTheEarliestMatch(t *testing.T) {
	got, matched := cutAtStopSequence("one END two STOP three", []string{"STOP", "END"})

	if matched != "END" {
		t.Fatalf("matched = %q, want the earliest sequence, not the first listed", matched)
	}
	if got != "one " {
		t.Errorf("text = %q, want the text before the earliest sequence", got)
	}
}

func TestCutAtStopSequenceLeavesTextWithoutAMatchAlone(t *testing.T) {
	const text = "a complete answer"

	for _, sequences := range [][]string{nil, {}, {"STOP"}, {""}} {
		got, matched := cutAtStopSequence(text, sequences)
		if matched != "" || got != text {
			t.Errorf("sequences %#v gave (%q, %q), want the text unchanged", sequences, got, matched)
		}
	}
}

// An empty sequence would match at offset zero and blank every completion.
func TestCutAtStopSequenceIgnoresAnEmptySequence(t *testing.T) {
	got, matched := cutAtStopSequence("hello STOP world", []string{"", "STOP"})

	if matched != "STOP" {
		t.Fatalf("matched = %q, want the real sequence", matched)
	}
	if got != "hello " {
		t.Errorf("text = %q, want the text before the real sequence", got)
	}
}

// drive feeds every chunk through the writer and returns what reached the wire.
func drive(writer *stopSequenceWriter, chunks ...string) string {
	var emitted strings.Builder
	for _, chunk := range chunks {
		emit, stopped := writer.next(chunk)
		emitted.WriteString(emit)
		if stopped {
			break
		}
	}
	emitted.WriteString(writer.flush())
	return emitted.String()
}

// A stop sequence split across two chunks used to reach the client as the first
// half of the sequence, because each chunk was emitted as it arrived.
func TestStopSequenceWriterHoldsBackASplitSequence(t *testing.T) {
	writer := newStopSequenceWriter([]string{"\n\nHuman:"})

	got := drive(writer, "answer\n\nHum", "an: next question")

	if got != "answer" {
		t.Errorf("emitted %q, want only the text before the sequence", got)
	}
	// The client is told which sequence ended the answer, not merely that one
	// did, because a request may carry several.
	if got := writer.matched(); got != "\n\nHuman:" {
		t.Errorf("matched = %q, want the sequence that fired", got)
	}
}

func TestStopSequenceWriterNamesNoSequenceWithoutAMatch(t *testing.T) {
	writer := newStopSequenceWriter([]string{"STOP"})

	drive(writer, "a complete answer")

	if got := writer.matched(); got != "" {
		t.Errorf("matched = %q, want no sequence", got)
	}
}

// OpenAI's stop is a single string or an array of them, so it arrives untyped
// and both shapes have to reach the same list.
func TestOpenAIStopSequencesAcceptsBothShapes(t *testing.T) {
	for _, c := range []struct {
		name string
		stop any
		want []string
	}{
		{"absent", nil, nil},
		{"single string", "STOP", []string{"STOP"}},
		{"array", []any{"A", "B"}, []string{"A", "B"}},
		{"empty string", "", nil},
		{"array with an empty entry", []any{"", "B"}, []string{"B"}},
		{"array with a non-string entry", []any{7, "B"}, []string{"B"}},
		// A malformed stop leaves the request answerable, so it is ignored
		// rather than refused.
		{"wrong type", 7, nil},
		{"object", map[string]any{"a": 1}, nil},
	} {
		got := openAIStopSequences(c.stop)
		if len(got) != len(c.want) {
			t.Errorf("%s: openAIStopSequences = %#v, want %#v", c.name, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%s: openAIStopSequences = %#v, want %#v", c.name, got, c.want)
				break
			}
		}
	}
}

// A client that tests the stop field for null would read an empty string as a
// sequence that fired.
func TestNullableStringReportsNothingAsNull(t *testing.T) {
	if got := nullableString(""); got != nil {
		t.Errorf("nullableString(\"\") = %#v, want nil", got)
	}
	if got := nullableString("STOP"); got != "STOP" {
		t.Errorf("nullableString = %#v, want the sequence", got)
	}
}

func TestStopSequenceWriterEmitsEverythingWithoutAMatch(t *testing.T) {
	writer := newStopSequenceWriter([]string{"\n\nHuman:"})

	got := drive(writer, "one ", "two ", "three")

	if got != "one two three" {
		t.Errorf("emitted %q, want the whole completion", got)
	}
	if writer.stoppedEarly() {
		t.Error("the writer reported a stop sequence that never arrived")
	}
}

// A request without stop sequences must not be delayed by a hold-back.
func TestStopSequenceWriterPassesChunksThroughWhenInactive(t *testing.T) {
	writer := newStopSequenceWriter(nil)

	if writer.active() {
		t.Error("a writer with no sequences reports itself active")
	}
	emit, stopped := writer.next("first")
	if emit != "first" || stopped {
		t.Errorf("next = (%q, %v), want the chunk released at once", emit, stopped)
	}
	if tail := writer.flush(); tail != "" {
		t.Errorf("flush = %q, want nothing held back", tail)
	}
}

// The hold-back is a byte count, so its boundary can fall inside a character.
func TestStopSequenceWriterNeverSplitsACharacter(t *testing.T) {
	// Two-byte characters, and a sequence long enough to hold back an odd
	// number of bytes.
	writer := newStopSequenceWriter([]string{"STOP"})

	emit, _ := writer.next(strings.Repeat("ş", 10))

	if !utf8.ValidString(emit) {
		t.Fatalf("emitted invalid UTF-8: %q", emit)
	}
	if got := emit + writer.flush(); got != strings.Repeat("ş", 10) {
		t.Errorf("the completion came back as %q", got)
	}
}

// The writer stops at the first sequence and releases nothing afterwards, so a
// caller that keeps reading the upstream cannot leak text past the stop.
func TestStopSequenceWriterStaysStoppedAfterAMatch(t *testing.T) {
	writer := newStopSequenceWriter([]string{"STOP"})

	if emit, stopped := writer.next("keep STOP drop"); emit != "keep " || !stopped {
		t.Fatalf("next = (%q, %v), want the text before the sequence", emit, stopped)
	}
	if emit, stopped := writer.next("more text"); emit != "" || !stopped {
		t.Errorf("a later chunk gave (%q, %v), want nothing", emit, stopped)
	}
	if tail := writer.flush(); tail != "" {
		t.Errorf("flush after a stop = %q, want nothing", tail)
	}
}
