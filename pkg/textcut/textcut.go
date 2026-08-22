// Package textcut cuts text on character boundaries.
//
// Every limit in this service is a byte count: a prompt budget, a log excerpt,
// a stored transcript, a captured command output. The text those limits apply
// to is whatever a model wrote, a tool printed or a backend returned, so a cut
// at an arbitrary byte lands inside a multi-byte character and replaces it with
// a byte no decoder accepts. Each package used to carry its own copy of the
// boundary search; they live here instead.
package textcut

import "unicode/utf8"

// Truncate returns value cut to at most limit bytes, never splitting a
// character. A value already within the limit is returned unchanged.
func Truncate[T string | []byte](value T, limit int) T {
	if limit >= len(value) {
		return value
	}
	if limit <= 0 {
		return value[:0]
	}
	return value[:StartAtOrBefore(value, limit)]
}

// StartAtOrBefore returns the largest index at or before i that starts a
// character, so text cut there keeps every character whole.
func StartAtOrBefore[T string | []byte](value T, i int) int {
	if i >= len(value) {
		return len(value)
	}
	if i < 0 {
		return 0
	}
	for i > 0 && !utf8.RuneStart(value[i]) {
		i--
	}
	return i
}

// StartAtOrAfter returns the smallest index at or after i that starts a
// character, so text beginning there starts on a whole character.
func StartAtOrAfter[T string | []byte](value T, i int) int {
	if i < 0 {
		return 0
	}
	for i < len(value) && !utf8.RuneStart(value[i]) {
		i++
	}
	return i
}
