package servers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/KilimcininKorOglu/M365Bridge/pkg/models"
)

// personalizationServer builds a server for the checks that never reach the
// backend. The upstream calls themselves are covered in pkg/client, against an
// httptest server; the endpoint they use is private to that package, so the
// tenant gate and the success shape are not reachable from here.
func personalizationServer() *APIServer {
	return &APIServer{config: &models.Config{UserOID: "oid", TenantID: "tid"}}
}

func TestPersonalizationRefusesAMethodItDoesNotServe(t *testing.T) {
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		recorder := httptest.NewRecorder()
		personalizationServer().handlePersonalization(recorder,
			httptest.NewRequest(method, "/v1/personalization", nil))

		if recorder.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s = %d, want 405", method, recorder.Code)
		}
	}
}

// The field is a pointer so an empty body cannot be read as a request to turn
// memory off. Turning off a setting nobody asked about is the one outcome this
// route must never produce by accident.
func TestPersonalizationRefusesAPatchThatNamesNothing(t *testing.T) {
	for name, body := range map[string]string{
		"empty object":    `{}`,
		"unrelated field": `{"insights_from_history_enabled": false}`,
	} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPatch, "/v1/personalization", strings.NewReader(body))
		personalizationServer().handlePersonalization(recorder, request)

		if recorder.Code != http.StatusBadRequest {
			t.Errorf("%s = %d, want 400", name, recorder.Code)
		}
		var response struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
			t.Fatalf("%s: decode: %v", name, err)
		}
		if !strings.Contains(response.Error.Message, "memory_enabled") {
			t.Errorf("%s: message %q does not name the field", name, response.Error.Message)
		}
	}
}

func TestPersonalizationRefusesAMalformedPatchBody(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPatch, "/v1/personalization", strings.NewReader("{not json"))
	personalizationServer().handlePersonalization(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
}

// The wire names are part of the contract the interface reads, so a rename has
// to break a test rather than an interface.
func TestPersonalizationJSONNamesEveryFlag(t *testing.T) {
	body := personalizationJSON(map[string]bool{
		"memory_enabled":                    false,
		"insights_from_history_enabled":     false,
		"custom_instruction_enabled":        true,
		"graph_content_enabled":             false,
		"personalization_allowed_by_tenant": true,
	})

	if body["object"] != "personalization" {
		t.Errorf("object = %v", body["object"])
	}
	for _, key := range []string{
		"memory_enabled", "insights_from_history_enabled",
		"custom_instruction_enabled", "graph_content_enabled",
		"personalization_allowed_by_tenant",
	} {
		if _, ok := body[key]; !ok {
			t.Errorf("body is missing %q", key)
		}
	}
}
