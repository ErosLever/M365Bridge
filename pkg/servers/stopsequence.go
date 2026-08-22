package servers

import (
	"strings"

	"github.com/KilimcininKorOglu/M365Bridge/pkg/textcut"
)

// cutAtStopSequence returns text ending just before the earliest occurrence of
// any stop sequence, and reports whether one was found. The sequence itself is
// removed, which is what both provider protocols specify: the caller asked the
// completion to end there, so the marker is not part of the answer.
//
// An empty sequence is ignored rather than matching at offset zero, which would
// blank every completion.
func cutAtStopSequence(text string, stopSequences []string) (string, bool) {
	cut := -1
	for _, sequence := range stopSequences {
		if sequence == "" {
			continue
		}
		if i := strings.Index(text, sequence); i >= 0 && (cut < 0 || i < cut) {
			cut = i
		}
	}
	if cut < 0 {
		return text, false
	}
	return text[:cut], true
}

// stopSequenceWriter releases streamed text only once it can no longer become
// the start of a stop sequence.
//
// A stop sequence can straddle two chunks, so a stream that emits each chunk as
// it arrives would send the first half of the sequence before recognizing it.
// The writer holds back the tail that could still complete one, which is at
// most one byte short of the longest sequence.
type stopSequenceWriter struct {
	sequences []string
	holdBack  int
	pending   string
	stopped   bool
}

// newStopSequenceWriter builds a writer for the request's stop sequences. With
// none, the writer passes every chunk straight through.
func newStopSequenceWriter(stopSequences []string) *stopSequenceWriter {
	writer := &stopSequenceWriter{}
	for _, sequence := range stopSequences {
		if sequence == "" {
			continue
		}
		writer.sequences = append(writer.sequences, sequence)
		writer.holdBack = max(writer.holdBack, len(sequence)-1)
	}
	return writer
}

// active reports whether any stop sequence is in force.
func (s *stopSequenceWriter) active() bool {
	return len(s.sequences) > 0
}

// stoppedEarly reports whether a stop sequence ended the completion.
func (s *stopSequenceWriter) stoppedEarly() bool {
	return s.stopped
}

// next takes the next upstream chunk and returns the text that is safe to emit
// now. The second result reports that a stop sequence was reached, after which
// the caller stops reading and emits nothing more.
func (s *stopSequenceWriter) next(chunk string) (string, bool) {
	if s.stopped {
		return "", true
	}
	if !s.active() {
		return chunk, false
	}

	s.pending += chunk
	if cut, found := cutAtStopSequence(s.pending, s.sequences); found {
		s.stopped = true
		s.pending = ""
		return cut, true
	}

	release := len(s.pending) - s.holdBack
	if release <= 0 {
		return "", false
	}
	// The hold-back is a byte count, so the boundary can land inside a
	// character. Releasing there would put an invalid byte on the wire.
	release = textcut.StartAtOrBefore(s.pending, release)
	emit := s.pending[:release]
	s.pending = s.pending[release:]
	return emit, false
}

// flush returns the held-back tail once the upstream ends without a stop
// sequence.
func (s *stopSequenceWriter) flush() string {
	if s.stopped {
		return ""
	}
	tail := s.pending
	s.pending = ""
	return tail
}
