package servers

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/KilimcininKorOglu/M365Bridge/pkg/payload"
)

func TestResponsesInputIsEmptyRecognisesAProbe(t *testing.T) {
	// responsesInputToMessages turns an empty input into one empty user
	// message, which is exactly what the probe looks like.
	if !responsesInputIsEmpty(responsesInputToMessages([]any{})) {
		t.Fatal("an empty input was not recognised as a probe")
	}
	if !responsesInputIsEmpty([]payload.Message{{Role: "user", Content: "  \n\t "}}) {
		t.Fatal("a whitespace-only input was not recognised as a probe")
	}
	if !responsesInputIsEmpty(nil) {
		t.Fatal("a nil input was not recognised as a probe")
	}
}

func TestResponsesInputIsEmptyRejectsRealWork(t *testing.T) {
	cases := map[string][]payload.Message{
		"text": {{Role: "user", Content: "hello"}},
		"image": {{Role: "user", Images: []payload.ImageData{
			{MediaType: "image/png", Base64: "AAAA"},
		}}},
		"tool call":   {{Role: "assistant", ToolCalls: []payload.ToolCallRecord{{ID: "call_1", Name: "read_file"}}}},
		"tool result": {{Role: "user", ToolResults: []payload.ToolResultRecord{{ID: "call_1", Content: "ok"}}}},
	}
	for name, messages := range cases {
		if responsesInputIsEmpty(messages) {
			t.Fatalf("%s input was mistaken for a probe", name)
		}
	}
}

func TestRespondResponsesProbeAnswersWithoutUpstream(t *testing.T) {
	api := &APIServer{}
	rec := httptest.NewRecorder()
	api.respondResponsesProbe(rec, "gpt-5.5-reasoning", false)

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("probe body is not JSON: %v", err)
	}
	if body["object"] != "response" {
		t.Fatalf("object = %v, want response", body["object"])
	}
	if body["status"] != "completed" {
		t.Fatalf("status = %v, want completed", body["status"])
	}
	if id, _ := body["id"].(string); !strings.HasPrefix(id, "resp_") {
		t.Fatalf("id = %q, want a resp_ prefix", id)
	}
}

func TestRespondResponsesProbeStreamsTheEnvelopeLifecycle(t *testing.T) {
	api := &APIServer{}
	rec := httptest.NewRecorder()
	api.respondResponsesProbe(rec, "gpt-5.5-reasoning", true)

	body := rec.Body.String()
	for _, event := range []string{"response.created", "response.in_progress", "response.completed"} {
		if !strings.Contains(body, event) {
			t.Fatalf("probe stream omits %s:\n%s", event, body)
		}
	}
	// Codex reads sequence_number to order the envelope events.
	if !strings.Contains(body, `"sequence_number":0`) || !strings.Contains(body, `"sequence_number":2`) {
		t.Fatalf("probe stream lost its sequence numbers:\n%s", body)
	}
}
