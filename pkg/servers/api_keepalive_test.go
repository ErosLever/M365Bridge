package servers

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/KilimcininKorOglu/M365Bridge/pkg/client"
)

func TestKeepaliveFramesMatchTheirWireFormat(t *testing.T) {
	recorder := httptest.NewRecorder()
	if err := writeSSEKeepalive(recorder, recorder); err != nil {
		t.Fatalf("openai keepalive returned %v", err)
	}
	// An SSE comment carries no field, so no client parses it as data.
	if got := recorder.Body.String(); got != ": keepalive\n\n" {
		t.Fatalf("openai keepalive = %q", got)
	}

	recorder = httptest.NewRecorder()
	if err := writeAnthropicKeepalive(recorder, recorder); err != nil {
		t.Fatalf("anthropic keepalive returned %v", err)
	}
	if got := recorder.Body.String(); got != "event: ping\ndata: {\"type\":\"ping\"}\n\n" {
		t.Fatalf("anthropic keepalive = %q", got)
	}
}

func TestNextStreamChunkWritesWhileUpstreamIsSilent(t *testing.T) {
	ch := make(chan client.StreamChunk, 1)
	keepalive := time.NewTicker(5 * time.Millisecond)
	defer keepalive.Stop()

	writes := make(chan struct{}, 16)
	go func() {
		// Let a few intervals pass before the first chunk arrives, which is
		// what a buffered tool-enabled turn looks like.
		time.Sleep(40 * time.Millisecond)
		ch <- client.StreamChunk{Text: "hello"}
	}()

	chunk, more := nextStreamChunk(ch, keepalive, httptest.NewRecorder(), func() error {
		writes <- struct{}{}
		return nil
	})
	if !more {
		t.Fatal("the channel reported closed while a chunk was pending")
	}
	if chunk.Text != "hello" {
		t.Fatalf("chunk text = %q, want hello", chunk.Text)
	}
	if len(writes) == 0 {
		t.Fatal("no keepalive was written during the silent wait")
	}
}

func TestNextStreamChunkStopsWhenTheKeepaliveWriteFails(t *testing.T) {
	// A client that stopped reading is only detected on the next write, so a
	// failed keepalive has to end the turn instead of looping forever.
	ch := make(chan client.StreamChunk)
	keepalive := time.NewTicker(5 * time.Millisecond)
	defer keepalive.Stop()

	if _, more := nextStreamChunk(ch, keepalive, httptest.NewRecorder(), func() error {
		return errors.New("connection reset by peer")
	}); more {
		t.Fatal("a failed keepalive write did not end the stream")
	}
}

func TestNextStreamChunkReportsAClosedChannel(t *testing.T) {
	ch := make(chan client.StreamChunk)
	close(ch)
	keepalive := time.NewTicker(time.Hour)
	defer keepalive.Stop()

	if _, more := nextStreamChunk(ch, keepalive, httptest.NewRecorder(), func() error {
		t.Fatal("a closed channel must not produce a keepalive")
		return nil
	}); more {
		t.Fatal("a closed channel reported more chunks")
	}
}

func TestNextStreamChunkStaysQuietOnABusyStream(t *testing.T) {
	ch := make(chan client.StreamChunk, 1)
	ch <- client.StreamChunk{Text: "now"}
	keepalive := time.NewTicker(time.Hour)
	defer keepalive.Stop()

	if _, more := nextStreamChunk(ch, keepalive, httptest.NewRecorder(), func() error {
		t.Fatal("a ready chunk must not produce a keepalive")
		return nil
	}); !more {
		t.Fatal("a ready chunk was reported as a closed channel")
	}
}

func TestRefreshStreamDeadlineToleratesAnUnsupportedWriter(t *testing.T) {
	// httptest.ResponseRecorder exposes no connection, so SetWriteDeadline
	// reports ErrNotSupported. A stream must not fail over that.
	var w http.ResponseWriter = httptest.NewRecorder()
	refreshStreamDeadline(w)
}
