package toolcalling

import (
	"encoding/json"
	"strings"
	"testing"
)

// decodeArgs mirrors how arguments reach the validator: as decoded JSON, where
// every number is a float64.
func decodeArgs(t *testing.T, raw string) map[string]any {
	t.Helper()
	var args map[string]any
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
	return args
}

func weatherSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"city":  map[string]any{"type": "string"},
			"days":  map[string]any{"type": "integer"},
			"units": map[string]any{"type": "string", "enum": []any{"metric", "imperial"}},
		},
		"required":             []any{"city"},
		"additionalProperties": false,
	}
}

func TestValidateAndPruneAcceptsValidArguments(t *testing.T) {
	args := decodeArgs(t, `{"city":"Istanbul","days":3,"units":"metric"}`)
	if err := ValidateAndPrune(args, weatherSchema()); err != nil {
		t.Fatalf("valid arguments rejected: %v", err)
	}
}

func TestValidateAndPruneRejectsContractViolations(t *testing.T) {
	cases := []struct {
		name string
		args string
	}{
		{"missing required", `{"days":3}`},
		{"wrong type", `{"city":42}`},
		{"non-integer", `{"city":"Istanbul","days":1.5}`},
		{"enum violation", `{"city":"Istanbul","units":"kelvin"}`},
	}
	for _, c := range cases {
		if err := ValidateAndPrune(decodeArgs(t, c.args), weatherSchema()); err == nil {
			t.Fatalf("%s: %s was accepted, want rejection", c.name, c.args)
		}
	}
}

func TestValidateAndPruneStripsUndeclaredArguments(t *testing.T) {
	// An otherwise correct call decorated with a stray field stays usable, so
	// the extra key is removed instead of costing a whole round trip.
	args := decodeArgs(t, `{"city":"Istanbul","nonsense":true}`)
	if err := ValidateAndPrune(args, weatherSchema()); err != nil {
		t.Fatalf("pruning path rejected the call: %v", err)
	}
	if _, present := args["nonsense"]; present {
		t.Fatalf("undeclared argument survived: %#v", args)
	}
	if args["city"] != "Istanbul" {
		t.Fatalf("declared argument was lost: %#v", args)
	}
}

func TestValidateAndPruneChecksNestedObjectsAndArrays(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"filter": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"tags": map[string]any{
						"type":  "array",
						"items": map[string]any{"type": "string"},
					},
				},
				"required": []any{"tags"},
			},
		},
		"required": []any{"filter"},
	}

	if err := ValidateAndPrune(decodeArgs(t, `{"filter":{"tags":["a","b"]}}`), schema); err != nil {
		t.Fatalf("valid nested arguments rejected: %v", err)
	}
	if err := ValidateAndPrune(decodeArgs(t, `{"filter":{"tags":["a",7]}}`), schema); err == nil {
		t.Fatal("array item of the wrong type was accepted")
	}
	if err := ValidateAndPrune(decodeArgs(t, `{"filter":{}}`), schema); err == nil {
		t.Fatal("nested missing required key was accepted")
	}
}

func TestValidateAndPruneAcceptsToolsWithoutSchema(t *testing.T) {
	// A tool declared without a schema states no contract, so anything passes.
	if err := ValidateAndPrune(decodeArgs(t, `{"anything":1}`), nil); err != nil {
		t.Fatalf("schemaless tool rejected: %v", err)
	}
}

// weatherContracts wires the weather schema into the parser's validation gate.
func weatherContracts() ToolContracts {
	return ContractsFor([]ToolDef{{
		Type:     "function",
		Function: ToolDefFunc{Name: "get_weather", Parameters: weatherSchema()},
	}})
}

func TestParseSimulatedResponseDropsSchemaViolation(t *testing.T) {
	// city must be a string. Forwarding this call would make the client's tool
	// fail, so the parser drops it and records why.
	raw := `{"choices":[{"message":{"role":"assistant","tool_calls":[
		{"id":"x","type":"function","function":{"name":"get_weather","arguments":"{\"city\":42}"}}]}}]}`

	result := ParseSimulatedResponse(raw, []string{"get_weather"}, weatherContracts())
	if len(result.ToolCalls) != 0 {
		t.Fatalf("a schema-violating tool call was forwarded: %#v", result.ToolCalls)
	}
	if len(result.DroppedCalls) != 1 || result.DroppedCalls[0].Name != "get_weather" {
		t.Fatalf("DroppedCalls = %#v, want one get_weather entry", result.DroppedCalls)
	}
	if result.DroppedCalls[0].Reason == "" {
		t.Fatal("dropped call carries no reason, so the repair note cannot explain the failure")
	}
}

func TestParseSimulatedResponsePrunesUndeclaredArguments(t *testing.T) {
	raw := `{"choices":[{"message":{"role":"assistant","tool_calls":[
		{"id":"x","type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"Istanbul\",\"stray\":1}"}}]}}]}`

	result := ParseSimulatedResponse(raw, []string{"get_weather"}, weatherContracts())
	if len(result.ToolCalls) != 1 {
		t.Fatalf("got %d tool calls, want the call kept after pruning", len(result.ToolCalls))
	}
	args := string(result.ToolCalls[0].Arguments)
	if strings.Contains(args, "stray") {
		t.Fatalf("undeclared argument survived into %s", args)
	}
	if !strings.Contains(args, "Istanbul") {
		t.Fatalf("declared argument was lost from %s", args)
	}
}

func TestParseSimulatedResponseAnthropicDropsSchemaViolation(t *testing.T) {
	raw := `{"content":[{"type":"tool_use","name":"get_weather","input":{"city":42}}]}`

	result := ParseSimulatedResponseAnthropic(raw, []string{"get_weather"}, weatherContracts())
	if len(result.ToolCalls) != 0 {
		t.Fatalf("a schema-violating tool_use was forwarded: %#v", result.ToolCalls)
	}
	if len(result.DroppedCalls) != 1 {
		t.Fatalf("DroppedCalls = %#v, want one entry", result.DroppedCalls)
	}
}

// twoToolContracts declares two schemaless tools so tool_choice is the only
// thing that can reject a call.
func twoToolContracts() ToolContracts {
	return ContractsFor([]ToolDef{
		{Type: "function", Function: ToolDefFunc{Name: "get_weather"}},
		{Type: "function", Function: ToolDefFunc{Name: "send_email"}},
	})
}

func weatherCallPayload() string {
	return `{"choices":[{"message":{"role":"assistant","tool_calls":[
		{"id":"x","type":"function","function":{"name":"get_weather","arguments":"{}"}}]}}]}`
}

func TestToolChoiceNoneDropsEveryCall(t *testing.T) {
	// The caller forbade tool use. A model that emits a call anyway must not
	// reach the client, and re-asking cannot change the answer, so the drop is
	// final rather than repairable.
	result := ParseSimulatedResponse(
		weatherCallPayload(),
		[]string{"get_weather", "send_email"},
		twoToolContracts().WithChoice("none"),
	)
	if len(result.ToolCalls) != 0 {
		t.Fatalf("tool_choice \"none\" still forwarded %#v", result.ToolCalls)
	}
	if len(result.DroppedCalls) != 0 {
		t.Fatalf("DroppedCalls = %#v, want none so the repair flow is not triggered", result.DroppedCalls)
	}
}

func TestToolChoicePinnedDropsOtherTools(t *testing.T) {
	// send_email is pinned, so a get_weather call is a contract violation the
	// model can plausibly fix when asked again.
	result := ParseSimulatedResponse(
		weatherCallPayload(),
		[]string{"get_weather", "send_email"},
		twoToolContracts().WithChoice("send_email"),
	)
	if len(result.ToolCalls) != 0 {
		t.Fatalf("pinned tool_choice still forwarded %#v", result.ToolCalls)
	}
	if len(result.DroppedCalls) != 1 || result.DroppedCalls[0].Name != "get_weather" {
		t.Fatalf("DroppedCalls = %#v, want one repairable get_weather entry", result.DroppedCalls)
	}
	if !strings.Contains(result.DroppedCalls[0].Reason, "send_email") {
		t.Fatalf("rejection reason %q does not name the pinned tool", result.DroppedCalls[0].Reason)
	}
}

func TestToolChoicePinnedKeepsTheChosenTool(t *testing.T) {
	result := ParseSimulatedResponse(
		weatherCallPayload(),
		[]string{"get_weather", "send_email"},
		twoToolContracts().WithChoice("get_weather"),
	)
	if len(result.ToolCalls) != 1 {
		t.Fatalf("the pinned tool was rejected: %#v / %#v", result.ToolCalls, result.DroppedCalls)
	}
}

func TestToolChoicePermissiveValuesAllowAnyTool(t *testing.T) {
	for _, choice := range []string{"", "auto", "required", "any"} {
		result := ParseSimulatedResponse(
			weatherCallPayload(),
			[]string{"get_weather", "send_email"},
			twoToolContracts().WithChoice(choice),
		)
		if len(result.ToolCalls) != 1 {
			t.Fatalf("tool_choice %q rejected a valid call: %#v", choice, result.DroppedCalls)
		}
	}
}

func TestToolChoiceAnthropicPinnedDropsOtherTools(t *testing.T) {
	raw := `{"content":[{"type":"tool_use","name":"get_weather","input":{}}]}`

	result := ParseSimulatedResponseAnthropic(
		raw,
		[]string{"get_weather", "send_email"},
		twoToolContracts().WithChoice("send_email"),
	)
	if len(result.ToolCalls) != 0 {
		t.Fatalf("pinned tool_choice still forwarded %#v", result.ToolCalls)
	}
	if len(result.DroppedCalls) != 1 {
		t.Fatalf("DroppedCalls = %#v, want one entry", result.DroppedCalls)
	}
}

func TestSchemaByToolReadsEveryProviderShape(t *testing.T) {
	anthropic := map[string]any{"type": "object", "properties": map[string]any{"a": map[string]any{"type": "string"}}}
	responses := map[string]any{"type": "object", "properties": map[string]any{"b": map[string]any{"type": "string"}}}
	openai := map[string]any{"type": "object", "properties": map[string]any{"c": map[string]any{"type": "string"}}}

	schemas := SchemaByTool([]ToolDef{
		{Name: "anthropic_tool", InputSchema: anthropic},
		{Type: "function", Name: "responses_tool", Parameters: responses},
		{Type: "function", Function: ToolDefFunc{Name: "openai_tool", Parameters: openai}},
		{Type: "function", Function: ToolDefFunc{Name: "bare_tool"}},
	})

	for name, key := range map[string]string{"anthropic_tool": "a", "responses_tool": "b", "openai_tool": "c"} {
		schema := schemas[name]
		if schema == nil {
			t.Fatalf("%s has no schema", name)
		}
		properties, _ := schema["properties"].(map[string]any)
		if _, ok := properties[key]; !ok {
			t.Fatalf("%s resolved to the wrong schema: %#v", name, schema)
		}
	}
	if schemas["bare_tool"] != nil {
		t.Fatalf("schemaless tool mapped to %#v, want nil", schemas["bare_tool"])
	}
}
