package servers

import (
	"strings"
	"testing"

	"github.com/KilimcininKorOglu/M365Bridge/pkg/payload"
)

func citySchemaFormat() map[string]any {
	return map[string]any{
		"type": "json_schema",
		"json_schema": map[string]any{
			"name":   "city",
			"strict": true,
			"schema": map[string]any{
				"type":       "object",
				"properties": map[string]any{"city": map[string]any{"type": "string"}},
				"required":   []any{"city"},
			},
		},
	}
}

func TestJSONObjectFormatAsksForJSON(t *testing.T) {
	instruction, ok := jsonModeInstruction(map[string]any{"type": "json_object"})

	if !ok {
		t.Fatal("json_object did not ask for JSON")
	}
	if instruction != jsonOnlyInstruction {
		t.Errorf("instruction = %q", instruction)
	}
}

// A json_schema request used to be accepted and then ignored, so a client that
// asked for a strict shape received prose.
func TestJSONSchemaFormatCarriesTheSchema(t *testing.T) {
	instruction, ok := jsonModeInstruction(citySchemaFormat())

	if !ok {
		t.Fatal("json_schema did not ask for JSON")
	}
	if !strings.HasPrefix(instruction, jsonOnlyInstruction) {
		t.Errorf("instruction lost the JSON-only demand: %q", instruction)
	}
	if !strings.Contains(instruction, `"properties":{"city":{"type":"string"}}`) {
		t.Errorf("instruction does not carry the schema: %q", instruction)
	}
}

// The wrapper is what carries the schema. Without one the demand for JSON still
// stands, because that is what the client asked for.
func TestJSONSchemaWithoutASchemaStillAsksForJSON(t *testing.T) {
	for _, format := range []map[string]any{
		{"type": "json_schema"},
		{"type": "json_schema", "json_schema": map[string]any{"name": "city"}},
	} {
		instruction, ok := jsonModeInstruction(format)
		if !ok || instruction != jsonOnlyInstruction {
			t.Errorf("format %#v gave (%q, %v)", format, instruction, ok)
		}
	}
}

func TestUnknownResponseFormatAsksForNothing(t *testing.T) {
	for _, format := range []map[string]any{
		nil,
		{},
		{"type": "text"},
		{"type": 7},
	} {
		if instruction, ok := jsonModeInstruction(format); ok || instruction != "" {
			t.Errorf("format %#v gave (%q, %v), want no instruction", format, instruction, ok)
		}
	}
}

func TestJSONModeAppendsToAnExistingSystemMessage(t *testing.T) {
	messages := []payload.Message{
		{Role: "system", Content: "You are terse."},
		{Role: "user", Content: "hello"},
	}

	injectJSONMode(&messages, jsonOnlyInstruction)

	if len(messages) != 2 {
		t.Fatalf("messages = %d, want the same two", len(messages))
	}
	if !strings.HasPrefix(messages[0].Content, "You are terse.") {
		t.Errorf("the existing system message was replaced: %q", messages[0].Content)
	}
	if !strings.Contains(messages[0].Content, jsonOnlyInstruction) {
		t.Errorf("the instruction did not reach the system message: %q", messages[0].Content)
	}
}

func TestJSONModePrependsASystemMessageWhenThereIsNone(t *testing.T) {
	messages := []payload.Message{{Role: "user", Content: "hello"}}

	injectJSONMode(&messages, jsonOnlyInstruction)

	if len(messages) != 2 {
		t.Fatalf("messages = %d, want a system message added", len(messages))
	}
	if messages[0].Role != "system" || messages[0].Content != jsonOnlyInstruction {
		t.Errorf("first message = %#v", messages[0])
	}
	if messages[1].Content != "hello" {
		t.Errorf("the user message was disturbed: %#v", messages[1])
	}
}
