// Package toolcalling provides simulated tool calling support for clients
// (Claude Code, Codex, etc.) by sending the entire request JSON to M365 Copilot
// and parsing the simulated response (OpenAI chat.completion or Anthropic
// Messages shape) from a ```json code block.
//
// M365 Copilot backend does not natively support client-defined tools.
// This package bridges the gap by:
//   - Building a simulated prompt that embeds the full request JSON
//   - Parsing the simulated JSON response (tool_calls / tool_use blocks)
//   - Converting tool role messages (OpenAI) and tool_result blocks (Anthropic)
//     back into text for the M365 backend
package toolcalling

import (
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
)

// ToolDef represents a tool definition from the client request.
type ToolDef struct {
	Type      string      `json:"type"`
	Namespace string      `json:"namespace,omitempty"`
	Function  ToolDefFunc `json:"function"`
	// Anthropic-style fields (flat, no "function" wrapper)
	Name        string         `json:"name,omitempty"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"input_schema,omitempty"`
	// Responses namespace definitions contain nested callable tools.
	Tools []ToolDef `json:"tools,omitempty"`
	// Responses-style function definitions keep parameters at the tool level.
	Parameters map[string]any `json:"parameters,omitempty"`
}

// ToolDefFunc is the OpenAI-style function definition inside a tool.
type ToolDefFunc struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

// MarshalJSON preserves the provider-specific tool definition shape. Anthropic
// tools are flat, OpenAI Chat Completions tools use function, and Responses
// function tools use top-level parameters.
func (t ToolDef) MarshalJSON() ([]byte, error) {
	type wireToolDef struct {
		Type        string         `json:"type,omitempty"`
		Namespace   string         `json:"namespace,omitempty"`
		Function    *ToolDefFunc   `json:"function,omitempty"`
		Name        string         `json:"name,omitempty"`
		Description string         `json:"description,omitempty"`
		InputSchema map[string]any `json:"input_schema,omitempty"`
		Tools       []ToolDef      `json:"tools,omitempty"`
		Parameters  map[string]any `json:"parameters,omitempty"`
	}

	wire := wireToolDef{
		Type:        t.Type,
		Namespace:   t.Namespace,
		Name:        t.Name,
		Description: t.Description,
		InputSchema: t.InputSchema,
		Tools:       t.Tools,
		Parameters:  t.Parameters,
	}
	if t.Function.Name != "" || t.Function.Description != "" || t.Function.Parameters != nil {
		function := t.Function
		wire.Function = &function
	}
	return json.Marshal(wire)
}

// ToolCall represents a parsed tool call from the M365 response.
type ToolCall struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Namespace string          `json:"namespace,omitempty"`
	Arguments json.RawMessage `json:"arguments"`
}

// nextToolCallID returns a unique tool call ID.
//
// The ID must be unique across every request and every restart. A sequential
// counter was both a data race between concurrent requests and a source of
// collisions: it restarted at zero on every boot and could repeat the
// positional fallback IDs, which made clients reject a turn with a duplicate
// tool call id.
func nextToolCallID() string {
	return "call_" + uuid.NewString()
}

// ToolName extracts the name from either OpenAI or Anthropic tool definition.
func ToolName(t *ToolDef) string {
	if t.Function.Name != "" {
		return t.Function.Name
	}
	return t.Name
}

// FormatToolResult converts a tool result (from the client) into text
// that the M365 backend can understand in the next message.
func FormatToolResult(toolCallID, toolName, result string) string {
	return fmt.Sprintf("[Tool Result for %s (call_id: %s)]\n%s", toolName, toolCallID, result)
}

// FormatAssistantToolCall converts a previous assistant tool call (from conversation
// history) into text that the M365 backend can understand.
func FormatAssistantToolCall(toolName string, arguments json.RawMessage) string {
	return fmt.Sprintf("[Previous Tool Call: %s]\nArguments: %s", toolName, string(arguments))
}
