package servers

import (
	"testing"
)

// A session and the conversation it points at are one thing to a caller.
// Deleting the conversation used to leave the mapping behind, so the session's
// next turn opened a new conversation under an id the caller had deleted.
func TestSessionsForFindsEverySessionBoundToOneConversation(t *testing.T) {
	cache := NewContextCache(t.TempDir())
	cache.Set(sessionKeyPrefix+"first", "conv-A")
	cache.Set(sessionKeyPrefix+"second", "conv-A")
	cache.Set(sessionKeyPrefix+"other", "conv-B")

	got := cache.SessionsFor("conv-A")

	if len(got) != 2 {
		t.Fatalf("SessionsFor returned %v, want both sessions bound to conv-A", got)
	}
	for _, sid := range got {
		if sid != "first" && sid != "second" {
			t.Errorf("SessionsFor returned %q, which is bound to another conversation", sid)
		}
	}
}

func TestSessionsForReportsNothingForAnUnboundConversation(t *testing.T) {
	cache := NewContextCache(t.TempDir())
	cache.Set(sessionKeyPrefix+"first", "conv-A")

	if got := cache.SessionsFor("conv-never"); len(got) != 0 {
		t.Errorf("SessionsFor = %v, want nothing", got)
	}
}

// A blank conversation ID would otherwise match every record whose mapping was
// never written, and clear sessions the caller never named.
func TestSessionsForRefusesABlankConversationID(t *testing.T) {
	cache := NewContextCache(t.TempDir())
	cache.Set(sessionKeyPrefix+"first", "conv-A")

	for _, blank := range []string{"", "   "} {
		if got := cache.SessionsFor(blank); len(got) != 0 {
			t.Errorf("SessionsFor(%q) = %v, want nothing", blank, got)
		}
	}
}

// Deleting the conversation must clear the mapping on this side too, or the
// two sides disagree about what exists.
//
// Two sessions are bound to the same conversation because that is what deleting
// through the session route relies on: it names one session, and the sibling
// that shares its conversation has to go with it.
func TestDropSessionsForClearsEverySessionOnThatConversation(t *testing.T) {
	api := &APIServer{ctxCache: NewContextCache(t.TempDir())}
	api.ctxCache.Set(sessionKeyPrefix+"bound", "conv-A")
	api.ctxCache.Set(sessionKeyPrefix+"sibling", "conv-A")
	api.ctxCache.Set(sessionKeyPrefix+"unrelated", "conv-B")

	api.dropSessionsFor("conv-A")

	if got := api.ctxCache.Get(sessionKeyPrefix + "bound"); got != "" {
		t.Errorf("the named session still maps to %q", got)
	}
	if got := api.ctxCache.Get(sessionKeyPrefix + "sibling"); got != "" {
		t.Errorf("the sibling session still maps to %q, and its conversation is gone", got)
	}
	if got := api.ctxCache.Get(sessionKeyPrefix + "unrelated"); got != "conv-B" {
		t.Errorf("an unrelated session was cleared; it maps to %q, want conv-B", got)
	}
}
