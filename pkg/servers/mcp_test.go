package servers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/KilimcininKorOglu/M365Bridge/pkg/models"
)

// postMCP sends one JSON-RPC body to the MCP handler and returns the decoded
// response.
func postMCP(t *testing.T, api *APIServer, body string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	rec := httptest.NewRecorder()
	api.handleMCP(rec, req)

	if rec.Body.Len() == 0 {
		return rec, nil
	}
	var decoded map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("decode response %q: %v", rec.Body.String(), err)
	}
	return rec, decoded
}

func newMCPTestServer() *APIServer {
	return &APIServer{config: &models.Config{}}
}

func TestMCPInitializeAdvertisesToolsCapability(t *testing.T) {
	api := newMCPTestServer()
	_, resp := postMCP(t, api, `{"jsonrpc":"2.0","id":1,"method":"initialize"}`)

	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("response has no result: %#v", resp)
	}
	if result["protocolVersion"] != mcpProtocolVersion {
		t.Fatalf("protocolVersion = %v, want %q", result["protocolVersion"], mcpProtocolVersion)
	}
	capabilities, ok := result["capabilities"].(map[string]any)
	if !ok {
		t.Fatalf("capabilities missing: %#v", result)
	}
	if _, ok := capabilities["tools"]; !ok {
		t.Fatalf("capabilities does not advertise tools: %#v", capabilities)
	}
}

func TestMCPToolsListExposesLeafToolsOnly(t *testing.T) {
	api := newMCPTestServer()
	_, resp := postMCP(t, api, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)

	result := resp["result"].(map[string]any)
	tools, ok := result["tools"].([]any)
	if !ok || len(tools) == 0 {
		t.Fatalf("tools/list returned no tools: %#v", result)
	}

	names := make(map[string]bool, len(tools))
	for _, raw := range tools {
		tool := raw.(map[string]any)
		names[tool["name"].(string)] = true
		schema, ok := tool["inputSchema"].(map[string]any)
		if !ok {
			t.Fatalf("tool %v has no inputSchema", tool["name"])
		}
		// Nesting the /v1 tool emulation inside MCP would create two tool loops,
		// so no tool may accept a tools parameter.
		properties := schema["properties"].(map[string]any)
		if _, leaked := properties["tools"]; leaked {
			t.Fatalf("tool %v exposes a tools parameter", tool["name"])
		}
	}
	for _, want := range []string{"ask_copilot", "describe_image"} {
		if !names[want] {
			t.Fatalf("tools/list is missing %q, got %v", want, names)
		}
	}
}

func TestMCPRejectsBadEnvelopeAndUnknownMethod(t *testing.T) {
	api := newMCPTestServer()

	if _, resp := postMCP(t, api, `{"jsonrpc":"1.0","id":3,"method":"initialize"}`); resp["error"] == nil {
		t.Fatalf("wrong jsonrpc version accepted: %#v", resp)
	}
	if _, resp := postMCP(t, api, `not json`); resp["error"] == nil {
		t.Fatalf("malformed body accepted: %#v", resp)
	}

	_, resp := postMCP(t, api, `{"jsonrpc":"2.0","id":4,"method":"does/not/exist"}`)
	errObject, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("unknown method returned no error: %#v", resp)
	}
	if int(errObject["code"].(float64)) != mcpErrMethodNotFound {
		t.Fatalf("error code = %v, want %d", errObject["code"], mcpErrMethodNotFound)
	}
}

func TestMCPNotificationGetsNoBody(t *testing.T) {
	api := newMCPTestServer()
	rec, _ := postMCP(t, api, `{"jsonrpc":"2.0","method":"notifications/initialized"}`)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusAccepted)
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("notification produced a body: %q", rec.Body.String())
	}
}

func TestMCPToolCallValidatesArguments(t *testing.T) {
	api := newMCPTestServer()

	// A missing prompt must fail inside the result, which is how MCP reports a
	// tool that ran and failed, rather than as a protocol error.
	_, resp := postMCP(t, api, `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"ask_copilot","arguments":{}}}`)
	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("tool call returned no result: %#v", resp)
	}
	if result["isError"] != true {
		t.Fatalf("missing prompt was not reported as a tool error: %#v", result)
	}

	_, resp = postMCP(t, api, `{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"nope","arguments":{}}}`)
	if resp["error"] == nil {
		t.Fatalf("unknown tool accepted: %#v", resp)
	}
}
