package servers

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestIncludeStreamUsageDefaultsToSending(t *testing.T) {
	// OpenAI defaults include_usage to false, but this proxy has always sent
	// usage on every streaming turn and clients here read it. Only an explicit
	// false may withhold it.
	if !includeStreamUsage(nil) {
		t.Fatal("an absent stream_options withheld usage")
	}
	if !includeStreamUsage(&streamOptions{}) {
		t.Fatal("stream_options without include_usage withheld usage")
	}
	enabled := true
	if !includeStreamUsage(&streamOptions{IncludeUsage: &enabled}) {
		t.Fatal("include_usage true withheld usage")
	}
	disabled := false
	if includeStreamUsage(&streamOptions{IncludeUsage: &disabled}) {
		t.Fatal("include_usage false still sent usage")
	}
}

func TestStreamOptionsDecodesTheOpenAIShape(t *testing.T) {
	var body struct {
		StreamOptions *streamOptions `json:"stream_options"`
	}
	if err := json.Unmarshal([]byte(`{"stream_options":{"include_usage":false}}`), &body); err != nil {
		t.Fatalf("decode stream_options: %v", err)
	}
	if includeStreamUsage(body.StreamOptions) {
		t.Fatal("a decoded include_usage false still sent usage")
	}
}

func TestSSEDoneOmitsUsageWhenWithheld(t *testing.T) {
	api := &APIServer{}
	recorder := httptest.NewRecorder()
	api.sendSSEDone(recorder, "chatcmpl-1", "gpt-5.5-reasoning", "stop", nil)

	body := recorder.Body.String()
	if strings.Contains(body, "usage") {
		t.Fatalf("a withheld usage still reached the wire:\n%s", body)
	}
	if !strings.Contains(body, `"finish_reason":"stop"`) {
		t.Fatalf("the final chunk lost its finish reason:\n%s", body)
	}
}
