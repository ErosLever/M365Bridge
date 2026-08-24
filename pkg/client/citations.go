package client

import "strings"

// M365 marks a citation inside answer text with Private Use Area delimiters:
//
//	U+E200 "cite" U+E202 "turn1search1" U+E201
//
// with U+E202 repeating for each further reference. The M365 web client turns
// these runs into numbered footnote links. Nothing here does, so the run
// reaches the caller as the literal word "cite" fused to a reference id, which
// is neither answer text nor anything a client can render. The delimiters are
// PUA code points, so they never occur in a real answer and a run is safe to
// remove outright.
//
// The same turn also delivers already-resolved markdown links through
// writeAtCursor, which is why an answer can carry both forms at once.
const (
	citationStart = '\ue200'
	citationEnd   = '\ue201'
)

// citationFilter removes citation runs from a stream of answer text.
//
// A run can straddle a chunk boundary, so text from an unterminated start
// delimiter onward is held back rather than emitted and repaired later: once a
// delta is on the wire it cannot be taken back.
type citationFilter struct {
	pending string
}

// push returns the text of s that is safe to emit now.
func (f *citationFilter) push(s string) string {
	if s == "" {
		return ""
	}
	combined := f.pending + s
	f.pending = ""

	var out strings.Builder
	out.Grow(len(combined))
	for {
		start := strings.IndexRune(combined, citationStart)
		if start < 0 {
			out.WriteString(combined)
			break
		}
		out.WriteString(combined[:start])
		rest := combined[start:]
		end := strings.IndexRune(rest, citationEnd)
		if end < 0 {
			// The run has not finished arriving. Hold it; the next chunk
			// either closes it or the turn ends and flush drops it.
			f.pending = rest
			break
		}
		combined = rest[end+len(string(citationEnd)):]
	}
	return out.String()
}

// flush reports what remains held at the end of a turn.
//
// An unterminated run is never answer text, so it is dropped rather than
// emitted. The return value exists so a caller can tell that something was
// discarded.
func (f *citationFilter) flush() string {
	held := f.pending
	f.pending = ""
	return held
}

// stripCitations removes every complete citation run from finished text.
//
// An unterminated run is dropped along with the rest of the string, because a
// start delimiter with no end means the remainder is a truncated marker.
func stripCitations(s string) string {
	if !strings.ContainsRune(s, citationStart) {
		return s
	}
	var filter citationFilter
	out := filter.push(s)
	filter.flush()
	return out
}
