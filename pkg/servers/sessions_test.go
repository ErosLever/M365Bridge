package servers

import (
	"encoding/json"
	"os"
	"testing"
)

func TestSetWritesTheRecordFormat(t *testing.T) {
	cache := NewContextCache(t.TempDir())
	cache.Set(sessionKeyPrefix+"dev-test-002", "conv-1")

	if got := cache.Get(sessionKeyPrefix + "dev-test-002"); got != "conv-1" {
		t.Fatalf("Get = %q, want conv-1", got)
	}

	data, err := os.ReadFile(cache.path(sessionKeyPrefix + "dev-test-002"))
	if err != nil {
		t.Fatalf("read cache file: %v", err)
	}
	var record sessionRecord
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatalf("decode cache file %s: %v", data, err)
	}
	if record.SessionID != "dev-test-002" {
		t.Fatalf("session_id = %q", record.SessionID)
	}
	if record.ConversationID != "conv-1" {
		t.Fatalf("conversation_id = %q", record.ConversationID)
	}
	if record.UpdatedAt == 0 {
		t.Fatal("the record carries no timestamp")
	}
}

// Every mapping written before the record format is a bare JSON string. Those
// files are live sessions, so losing them would silently restart every
// conversation.
func TestGetStillReadsTheLegacyFormat(t *testing.T) {
	dir := t.TempDir()
	cache := NewContextCache(dir)
	legacy, _ := json.Marshal("conv-legacy")
	if err := os.WriteFile(cache.path(sessionKeyPrefix+"old-session"), legacy, 0600); err != nil {
		t.Fatalf("write legacy file: %v", err)
	}

	if got := cache.Get(sessionKeyPrefix + "old-session"); got != "conv-legacy" {
		t.Fatalf("Get = %q, want conv-legacy", got)
	}
}

func TestListSortsNewestFirstAndCountsLegacyEntries(t *testing.T) {
	dir := t.TempDir()
	cache := NewContextCache(dir)

	for _, s := range []struct {
		id        string
		conv      string
		updatedAt int64
	}{
		{"a", "conv-a", 100},
		{"b", "conv-b", 300},
		{"c", "conv-c", 200},
	} {
		data, _ := json.Marshal(sessionRecord{SessionID: s.id, ConversationID: s.conv, UpdatedAt: s.updatedAt})
		if err := os.WriteFile(cache.path(sessionKeyPrefix+s.id), data, 0600); err != nil {
			t.Fatalf("write record: %v", err)
		}
	}
	legacy, _ := json.Marshal("conv-legacy")
	for _, name := range []string{"old-1", "old-2"} {
		if err := os.WriteFile(cache.path(sessionKeyPrefix+name), legacy, 0600); err != nil {
			t.Fatalf("write legacy record: %v", err)
		}
	}

	records, legacyCount := cache.List()
	if legacyCount != 2 {
		t.Fatalf("legacy count = %d, want 2", legacyCount)
	}
	if len(records) != 3 {
		t.Fatalf("listed %d records, want 3", len(records))
	}
	for i, want := range []string{"b", "c", "a"} {
		if records[i].SessionID != want {
			t.Fatalf("record %d = %q, want %q (newest first)", i, records[i].SessionID, want)
		}
	}
}

func TestLookupReportsAConversationAndATimestamp(t *testing.T) {
	cache := NewContextCache(t.TempDir())
	cache.Set(sessionKeyPrefix+"dev-test-002", "conv-1")

	record, ok := cache.Lookup("dev-test-002")
	if !ok {
		t.Fatal("Lookup reported a stored session as missing")
	}
	if record.ConversationID != "conv-1" || record.UpdatedAt == 0 {
		t.Fatalf("record = %#v", record)
	}

	if _, ok := cache.Lookup("never-used"); ok {
		t.Fatal("Lookup invented a session that was never stored")
	}
}

// A legacy entry carries no timestamp of its own, so the file modification
// time has to stand in; a zero would read as the epoch.
func TestLookupFallsBackToTheFileTime(t *testing.T) {
	cache := NewContextCache(t.TempDir())
	legacy, _ := json.Marshal("conv-legacy")
	if err := os.WriteFile(cache.path(sessionKeyPrefix+"old-session"), legacy, 0600); err != nil {
		t.Fatalf("write legacy file: %v", err)
	}

	record, ok := cache.Lookup("old-session")
	if !ok {
		t.Fatal("Lookup missed a legacy entry")
	}
	if record.UpdatedAt == 0 {
		t.Fatal("a legacy entry reported no timestamp")
	}
}
