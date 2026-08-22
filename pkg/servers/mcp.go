// This file implements a Model Context Protocol (MCP) server that exposes M365
// Copilot as a tool an MCP client can consult.
//
// Copilot is deliberately a LEAF here. The proxy's /v1 tool-calling emulation
// is not offered through MCP: an MCP client already has a real, schema-enforced
// tool mechanism, and steering Copilot into emitting an emulated tool call that
// comes back as plain text would nest two tool loops and produce incoherent
// results. Every MCP tool below is a single stateless turn.
package servers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/KilimcininKorOglu/M365Bridge/pkg/logging"
	"github.com/KilimcininKorOglu/M365Bridge/pkg/models"
	"github.com/KilimcininKorOglu/M365Bridge/pkg/payload"
)

// mcpProtocolVersion is the MCP revision this server implements.
const mcpProtocolVersion = "2025-06-18"

// JSON-RPC 2.0 error codes used by this server.
const (
	mcpErrParse          = -32700
	mcpErrInvalidRequest = -32600
	mcpErrMethodNotFound = -32601
	mcpErrInvalidParams  = -32602
	mcpErrInternal       = -32603
)

// mcpRequest is an incoming JSON-RPC 2.0 request. A request without an ID is a
// notification and receives no response body.
type mcpRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

// mcpErrorObject is the JSON-RPC error payload.
type mcpErrorObject struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// mcpToolCallParams is the params object of a tools/call request.
type mcpToolCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// mcpToolDefinitions returns the tools/list payload.
//
// No conversation_id argument is exposed: every call is an independent Copilot
// turn, and each description says so instead of implying continuity the server
// does not provide. The /v1 endpoints infer continuity from a resent messages
// array, which has no equivalent when an MCP caller sends a single prompt.
func mcpToolDefinitions() []map[string]any {
	return []map[string]any{
		{
			"name": "ask_copilot",
			"description": "Ask Microsoft 365 Copilot a question and get its text answer. " +
				"Copilot may use its own web search and code interpreter. " +
				"Each call is an independent, stateless turn.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"prompt": map[string]any{
						"type":        "string",
						"description": "The question or instruction for Copilot.",
					},
					"model": map[string]any{
						"type": "string",
						"description": "Optional model key, for example gpt5.5-reasoning or claude-sonnet. " +
							"Defaults to gpt5.5-reasoning.",
					},
				},
				"required":             []string{"prompt"},
				"additionalProperties": false,
			},
		},
		{
			"name": "describe_image",
			"description": "Ask Microsoft 365 Copilot about an image. Provide the image as a data URI " +
				"(data:image/png;base64,...). Each call is an independent, stateless turn.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"image_url": map[string]any{
						"type":        "string",
						"description": "A data URI holding the image bytes.",
					},
					"prompt": map[string]any{
						"type":        "string",
						"description": "What to ask about the image. Defaults to a general description.",
					},
				},
				"required":             []string{"image_url"},
				"additionalProperties": false,
			},
		},
	}
}

// handleMCP serves the Model Context Protocol over JSON-RPC 2.0.
func (api *APIServer) handleMCP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		api.handleCORS(w, r)
		return
	}
	if r.Method != http.MethodPost {
		api.sendError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// The MCP body carries a tool call's arguments, so it is bounded like the
	// chat routes rather than left to grow.
	limitRequestBody(w, r, requestBodyMax)
	var req mcpRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		api.sendMCPError(w, nil, mcpErrParse, "invalid JSON")
		return
	}
	if req.JSONRPC != "2.0" {
		api.sendMCPError(w, req.ID, mcpErrInvalidRequest, "jsonrpc must be \"2.0\"")
		return
	}

	logging.Infof("handleMCP: method=%s", req.Method)

	// A request without an ID is a notification: acknowledge without a body.
	isNotification := req.ID == nil

	switch req.Method {
	case "initialize":
		api.sendMCPResult(w, req.ID, map[string]any{
			"protocolVersion": mcpProtocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo": map[string]any{
				"name":    "m365bridge",
				"version": models.Version,
			},
		})
	case "tools/list":
		api.sendMCPResult(w, req.ID, map[string]any{"tools": mcpToolDefinitions()})
	case "tools/call":
		api.handleMCPToolCall(w, req)
	case "ping":
		api.sendMCPResult(w, req.ID, map[string]any{})
	default:
		if isNotification {
			// Lifecycle notifications such as notifications/initialized carry
			// no response.
			w.WriteHeader(http.StatusAccepted)
			return
		}
		api.sendMCPError(w, req.ID, mcpErrMethodNotFound, "unknown method: "+req.Method)
	}
}

// handleMCPToolCall dispatches a tools/call request to the named tool.
func (api *APIServer) handleMCPToolCall(w http.ResponseWriter, req mcpRequest) {
	var params mcpToolCallParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		api.sendMCPError(w, req.ID, mcpErrInvalidParams, "invalid params")
		return
	}

	var (
		text string
		err  error
	)
	switch params.Name {
	case "ask_copilot":
		text, err = api.mcpAskCopilot(params.Arguments)
	case "describe_image":
		text, err = api.mcpDescribeImage(params.Arguments)
	default:
		api.sendMCPError(w, req.ID, mcpErrInvalidParams, "unknown tool: "+params.Name)
		return
	}

	if err != nil {
		// Tool failures are reported inside the result with isError, which is
		// how MCP tells the calling model the tool ran and failed.
		logging.Errorf("handleMCPToolCall: tool=%s failed: %v", params.Name, err)
		api.sendMCPResult(w, req.ID, map[string]any{
			"content": []map[string]any{{"type": "text", "text": err.Error()}},
			"isError": true,
		})
		return
	}

	api.sendMCPResult(w, req.ID, map[string]any{
		"content": []map[string]any{{"type": "text", "text": text}},
		"isError": false,
	})
}

// mcpAskCopilot runs one stateless Copilot turn for a text prompt.
func (api *APIServer) mcpAskCopilot(rawArgs json.RawMessage) (string, error) {
	var args struct {
		Prompt string `json:"prompt"`
		Model  string `json:"model"`
	}
	if len(rawArgs) > 0 {
		if err := json.Unmarshal(rawArgs, &args); err != nil {
			return "", fmt.Errorf("invalid arguments: %w", err)
		}
	}
	if strings.TrimSpace(args.Prompt) == "" {
		return "", fmt.Errorf("prompt is required")
	}

	messages := []payload.Message{{Role: "user", Content: args.Prompt}}
	return api.mcpChat(messages, args.Model)
}

// mcpDescribeImage runs one stateless Copilot turn over an inline image.
func (api *APIServer) mcpDescribeImage(rawArgs json.RawMessage) (string, error) {
	var args struct {
		ImageURL string `json:"image_url"`
		Prompt   string `json:"prompt"`
		Model    string `json:"model"`
	}
	if len(rawArgs) > 0 {
		if err := json.Unmarshal(rawArgs, &args); err != nil {
			return "", fmt.Errorf("invalid arguments: %w", err)
		}
	}
	if strings.TrimSpace(args.ImageURL) == "" {
		return "", fmt.Errorf("image_url is required")
	}
	prompt := args.Prompt
	if strings.TrimSpace(prompt) == "" {
		prompt = "Describe this image."
	}

	// Reuse the provider content-block decoder rather than re-implementing data
	// URI parsing, so image handling stays identical to the /v1 endpoints.
	blocks, err := json.Marshal(map[string]any{
		"role": "user",
		"content": []any{
			map[string]any{"type": "text", "text": prompt},
			map[string]any{"type": "image_url", "image_url": map[string]any{"url": args.ImageURL}},
		},
	})
	if err != nil {
		return "", fmt.Errorf("failed to build message: %w", err)
	}
	var message payload.Message
	if err := json.Unmarshal(blocks, &message); err != nil {
		return "", fmt.Errorf("failed to decode image message: %w", err)
	}
	if len(message.Images) == 0 {
		return "", fmt.Errorf("image_url must be a data URI holding image bytes")
	}

	return api.mcpChat([]payload.Message{message}, args.Model)
}

// mcpChat sends one turn upstream with an empty conversation ID, keeping every
// MCP call independent.
func (api *APIServer) mcpChat(messages []payload.Message, modelKey string) (string, error) {
	api.mu.RLock()
	m365Client := api.m365Client
	api.mu.RUnlock()
	if m365Client == nil {
		return "", fmt.Errorf("server is not ready")
	}

	resolvedKey, _ := parseModelSessionID(modelKey)
	cfg, ok := models.FindModel(resolvedKey)
	if !ok {
		return "", fmt.Errorf("unknown model %q; GET /v1/models lists the available ids", resolvedKey)
	}

	text, _, _, _, _, err := m365Client.ChatConversation(
		messages,
		cfg.Tone,
		cfg.Override,
		"",
		api.config.UserOID,
		api.config.TenantID,
		false,
	)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(text) == "" {
		if api.quotaExhausted() {
			return "", fmt.Errorf("M365 conversation message quota exhausted")
		}
		return "", fmt.Errorf("M365 returned an empty response")
	}
	return text, nil
}

// sendMCPResult writes a JSON-RPC success response.
func (api *APIServer) sendMCPResult(w http.ResponseWriter, id any, result map[string]any) {
	api.sendJSON(w, http.StatusOK, map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"result":  result,
	})
}

// sendMCPError writes a JSON-RPC error response. JSON-RPC transports errors in
// the body, so the HTTP status stays 200.
func (api *APIServer) sendMCPError(w http.ResponseWriter, id any, code int, message string) {
	api.sendJSON(w, http.StatusOK, map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"error":   mcpErrorObject{Code: code, Message: message},
	})
}
