package toolcalling

import (
	"strings"
	"sync"
	"testing"
)

// TestNextToolCallIDIsUniqueUnderConcurrency guards the tool call ID contract.
// A sequential counter raced between request goroutines and repeated IDs after
// a restart, which made clients reject a turn with a duplicate tool call id.
func TestNextToolCallIDIsUniqueUnderConcurrency(t *testing.T) {
	const goroutines = 16
	const perGoroutine = 64

	var mu sync.Mutex
	seen := make(map[string]bool, goroutines*perGoroutine)

	var wg sync.WaitGroup
	for range goroutines {
		wg.Go(func() {
			local := make([]string, 0, perGoroutine)
			for range perGoroutine {
				local = append(local, nextToolCallID())
			}
			mu.Lock()
			defer mu.Unlock()
			for _, id := range local {
				if seen[id] {
					t.Errorf("duplicate tool call ID %q", id)
					return
				}
				seen[id] = true
			}
		})
	}
	wg.Wait()

	if len(seen) != goroutines*perGoroutine {
		t.Fatalf("got %d unique IDs, want %d", len(seen), goroutines*perGoroutine)
	}
}

func TestNextToolCallIDKeepsCallPrefix(t *testing.T) {
	id := nextToolCallID()
	if !strings.HasPrefix(id, "call_") {
		t.Fatalf("ID %q does not start with call_", id)
	}
	// A positional fallback such as call_0 must no longer be producible, since
	// those values collided with counter-generated IDs.
	if suffix := strings.TrimPrefix(id, "call_"); len(suffix) < 32 {
		t.Fatalf("ID %q is too short to be collision resistant", id)
	}
}
