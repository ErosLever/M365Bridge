package servers

import "testing"

// Every Response object the format defines carries created_at, including the
// partial one on response.created. The lifecycle events used to send only id,
// object, status and model, so a client reading the field from the first event
// found nothing.
func TestStatusObjectCarriesCreatedAt(t *testing.T) {
	got := responsesStatusObject("resp_test", "gpt-test", "in_progress", 1700000000)

	for field, want := range map[string]any{
		"id":         "resp_test",
		"object":     "response",
		"created_at": int64(1700000000),
		"status":     "in_progress",
		"model":      "gpt-test",
	} {
		if got[field] != want {
			t.Errorf("%s = %#v, want %#v", field, got[field], want)
		}
	}
}

func TestFailedEventCarriesCreatedAt(t *testing.T) {
	event := buildResponsesFailedEvent("resp_test", 1700000000, "gpt-test", "upstream_timeout", "timed out", 0)

	response, ok := event["response"].(map[string]any)
	if !ok {
		t.Fatalf("response payload has wrong type: %#v", event["response"])
	}
	if response["created_at"] != int64(1700000000) {
		t.Errorf("created_at = %#v, want the response's own time", response["created_at"])
	}
}

// created_at names the response, not the event. A time read per event would
// let a client see one response created twice, at two different instants.
func TestEveryEventOfOneResponseReportsTheSameCreatedAt(t *testing.T) {
	responseID, createdAt := newResponsesIdentity()

	created := responsesStatusObject(responseID, "gpt-test", "in_progress", createdAt)
	inProgress := responsesStatusObject(responseID, "gpt-test", "in_progress", createdAt)
	completed := buildResponsesObject(responseID, createdAt, "gpt-test", "done", "", nil, nil, false, "stop", 1, 1, 0)
	failed, _ := buildResponsesFailedEvent(responseID, createdAt, "gpt-test", "code", "message", 0)["response"].(map[string]any)

	for name, object := range map[string]map[string]any{
		"response.created":     created,
		"response.in_progress": inProgress,
		"response.completed":   completed,
		"response.failed":      failed,
	} {
		if object["created_at"] != createdAt {
			t.Errorf("%s created_at = %#v, want %#v", name, object["created_at"], createdAt)
		}
		if object["id"] != responseID {
			t.Errorf("%s id = %#v, want %#v", name, object["id"], responseID)
		}
	}
}

// The identity is one value pair per response, so two responses never share it.
func TestEachResponseGetsItsOwnIdentity(t *testing.T) {
	firstID, _ := newResponsesIdentity()
	secondID, _ := newResponsesIdentity()

	if firstID == secondID {
		t.Errorf("two responses share the id %q", firstID)
	}
}

func TestCompactionResponseCarriesTheMintedCreatedAt(t *testing.T) {
	got := buildCompactionResponseObject("resp_test", 1700000000, "gpt-test", "summary", 1, 1)

	if got["created_at"] != int64(1700000000) {
		t.Errorf("created_at = %#v, want the response's own time", got["created_at"])
	}
}
