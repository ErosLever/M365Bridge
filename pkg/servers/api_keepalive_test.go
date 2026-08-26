package servers

import (
	"context"
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

	recorder := httptest.NewRecorder()
	chunk, more := nextStreamChunk(context.Background(), ch, keepalive, recorder, recorder, func() error {
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

	recorder := httptest.NewRecorder()
	if _, more := nextStreamChunk(context.Background(), ch, keepalive, recorder, recorder, func() error {
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

	recorder := httptest.NewRecorder()
	if _, more := nextStreamChunk(context.Background(), ch, keepalive, recorder, recorder, func() error {
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

	recorder := httptest.NewRecorder()
	if _, more := nextStreamChunk(context.Background(), ch, keepalive, recorder, recorder, func() error {
		t.Fatal("a ready chunk must not produce a keepalive")
		return nil
	}); !more {
		t.Fatal("a ready chunk was reported as a closed channel")
	}
}

// A notice tells a reader the turn is generating a picture and will stay silent
// for a minute or more. It is written here so every streaming responder reports
// one without a line of its own, and it is consumed rather than returned,
// because it is not answer content and no responder should have to skip it.
func TestNextStreamChunkWritesANoticeAndKeepsWaiting(t *testing.T) {
	ch := make(chan client.StreamChunk, 2)
	ch <- client.StreamChunk{Notice: client.NoticeImageGenerating}
	ch <- client.StreamChunk{Text: "Buyur"}
	keepalive := time.NewTicker(time.Hour)
	defer keepalive.Stop()

	recorder := httptest.NewRecorder()
	chunk, more := nextStreamChunk(context.Background(), ch, keepalive, recorder, recorder, func() error {
		t.Fatal("a ready chunk must not produce a keepalive")
		return nil
	})
	if !more {
		t.Fatal("the stream ended on a notice")
	}
	if chunk.Notice != "" {
		t.Fatalf("a notice reached the responder as chunk %+v", chunk)
	}
	if chunk.Text != "Buyur" {
		t.Fatalf("chunk text = %q, want the chunk after the notice", chunk.Text)
	}
	// An SSE comment enters no field contract, so no client parses it as data.
	if got := recorder.Body.String(); got != ": notice image_generating\n\n" {
		t.Fatalf("notice frame = %q", got)
	}
}

func TestNextStreamChunkStopsWhenTheNoticeWriteFails(t *testing.T) {
	ch := make(chan client.StreamChunk, 1)
	ch <- client.StreamChunk{Notice: client.NoticeImageGenerating}
	keepalive := time.NewTicker(time.Hour)
	defer keepalive.Stop()

	if _, more := nextStreamChunk(context.Background(), ch, keepalive, failingWriter{}, failingWriter{}, func() error {
		return nil
	}); more {
		t.Fatal("a failed notice write did not end the stream")
	}
}

// failingWriter is a ResponseWriter whose every write fails, which is what a
// client that hung up looks like from inside a handler.
type failingWriter struct{}

func (failingWriter) Header() http.Header       { return http.Header{} }
func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("connection reset by peer") }
func (failingWriter) WriteHeader(int)           {}
func (failingWriter) Flush()                    {}

func TestRefreshStreamDeadlineToleratesAnUnsupportedWriter(t *testing.T) {
	// httptest.ResponseRecorder exposes no connection, so SetWriteDeadline
	// reports ErrNotSupported. A stream must not fail over that.
	var w http.ResponseWriter = httptest.NewRecorder()
	refreshStreamDeadline(w)
}

func TestNextStreamChunkStopsOnACanceledRequest(t *testing.T) {
	// The client hung up mid-turn. The handler must stop instead of writing
	// into a dead socket until the upstream finishes.
	ch := make(chan client.StreamChunk)
	keepalive := time.NewTicker(time.Hour)
	defer keepalive.Stop()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	recorder := httptest.NewRecorder()
	if _, more := nextStreamChunk(ctx, ch, keepalive, recorder, recorder, func() error {
		t.Fatal("a canceled request must not produce a keepalive")
		return nil
	}); more {
		t.Fatal("a canceled request reported more chunks")
	}
}
