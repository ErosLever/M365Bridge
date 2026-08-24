package client

import "testing"

func TestSnapshotDeltaEmitsTheFirstSnapshotWhenNothingWentOut(t *testing.T) {
	chunk, advanced := snapshotDelta("", "A **")
	if !advanced || chunk != "A **" {
		t.Fatalf("chunk=%q advanced=%v; a turn that never streams needs this snapshot", chunk, advanced)
	}
}

func TestSnapshotDeltaEmitsOnlyTheExtension(t *testing.T) {
	chunk, advanced := snapshotDelta("As of today, ", "As of today, Go 1.27.0 is current.")
	if !advanced || chunk != "Go 1.27.0 is current." {
		t.Fatalf("chunk=%q advanced=%v", chunk, advanced)
	}
}

func TestSnapshotDeltaIgnoresAnUnchangedSnapshot(t *testing.T) {
	if chunk, advanced := snapshotDelta("same", "same"); advanced || chunk != "" {
		t.Fatalf("chunk=%q advanced=%v; a repeated snapshot must not re-emit", chunk, advanced)
	}
}

// The measured failure. writeAtCursor delivered the answer with resolved
// citation links; the trailing snapshot restates it with raw markers, so it is
// shorter and diverges mid-string while ending identically. Emitting it sent
// the whole answer a second time.
func TestSnapshotDeltaDropsAReEncodedSnapshot(t *testing.T) {
	emitted := "As of 2026, Go [1](https://go.dev/doc/devel/release) was introduced in **Go 1.27**."
	snapshot := "As of 2026, Go citeturn1search1 was introduced in **Go 1.27**."
	if len(snapshot) >= len(emitted) {
		t.Fatalf("fixture is wrong: the raw-marker form must be shorter, got %d vs %d", len(snapshot), len(emitted))
	}

	chunk, advanced := snapshotDelta(emitted, snapshot)
	if advanced || chunk != "" {
		t.Fatalf("chunk=%q advanced=%v; the re-encoded snapshot duplicates delivered text", chunk, advanced)
	}
}

// The baseline must stay on the longer emitted text, because the next
// writeAtCursor delta appends to what actually went out. Adopting the shorter
// snapshot would make every later prefix test compare against text the client
// never received.
func TestSnapshotDeltaKeepsTheEmittedBaselineAfterDivergence(t *testing.T) {
	emitted := "aaaaXbbbb"
	if _, advanced := snapshotDelta(emitted, "aaaaYbbb"); advanced {
		t.Fatal("a diverging snapshot was allowed to become the baseline")
	}
}

// A snapshot that diverges while nothing has been emitted is still the answer.
func TestSnapshotDeltaTreatsAnEmptyBaselineAsNoConflict(t *testing.T) {
	chunk, advanced := snapshotDelta("", "totally different")
	if !advanced || chunk != "totally different" {
		t.Fatalf("chunk=%q advanced=%v", chunk, advanced)
	}
}
