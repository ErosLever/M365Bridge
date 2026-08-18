package toolcalling

import (
	"encoding/json"
	"fmt"
	"math"
)

// SchemaByTool maps each declared tool name to its JSON schema. It supports the
// three provider shapes a ToolDef can carry: Anthropic `input_schema`,
// Responses top-level `parameters`, and OpenAI Chat Completions
// `function.parameters`. Tools without a schema map to nil.
func SchemaByTool(tools []ToolDef) map[string]map[string]any {
	out := make(map[string]map[string]any, len(tools))
	for i := range tools {
		name := ToolName(&tools[i])
		if name == "" {
			continue
		}
		for _, schema := range []map[string]any{
			tools[i].InputSchema,
			tools[i].Parameters,
			tools[i].Function.Parameters,
		} {
			if len(schema) > 0 {
				out[name] = schema
				break
			}
		}
		if _, ok := out[name]; !ok {
			out[name] = nil
		}
	}
	return out
}

// ToolContracts carries everything the parser needs to validate a tool call:
// the required argument names and the full JSON schema, both keyed by tool
// name. It travels as one value so every parse path enforces the same contract.
type ToolContracts struct {
	Required map[string][]string
	Schemas  map[string]map[string]any
}

// ContractsFor builds the validation contracts for the declared tools.
func ContractsFor(tools []ToolDef) ToolContracts {
	return ToolContracts{
		Required: RequiredArgsByTool(tools),
		Schemas:  SchemaByTool(tools),
	}
}

// validate checks one tool call's arguments. It returns the arguments to
// forward, which may have had undeclared keys pruned, and a rejection reason
// that is empty when the call is acceptable.
func (c ToolContracts) validate(name string, arguments json.RawMessage) (result json.RawMessage, reason string) {
	if !toolCallSatisfiesRequired(arguments, c.Required[name]) {
		return arguments, "required arguments were missing or empty"
	}

	schema := c.Schemas[name]
	if len(schema) == 0 {
		return arguments, ""
	}

	var decoded map[string]any
	if err := json.Unmarshal(arguments, &decoded); err != nil {
		// Only an object can be validated. A non-object payload already failed
		// the required check unless the tool declares no required arguments.
		return arguments, "arguments were not a JSON object"
	}
	if err := ValidateAndPrune(decoded, schema); err != nil {
		return arguments, err.Error()
	}

	pruned, err := json.Marshal(decoded)
	if err != nil {
		return arguments, ""
	}
	return pruned, ""
}

// ValidateAndPrune checks arguments against a JSON schema and removes
// properties the schema does not declare when it sets
// `additionalProperties: false`.
//
// Pruning rather than rejecting keeps a call the model got right in substance
// but decorated with an extra field, which would otherwise be dropped and cost
// a whole round trip. Everything else is a genuine contract violation: a wrong
// type or an out-of-enum value would make the client's tool misbehave, so the
// call is reported invalid instead of being forwarded.
//
// A nil schema accepts any arguments, because a tool declared without one
// states no contract to enforce.
func ValidateAndPrune(args map[string]any, schema map[string]any) error {
	if len(schema) == 0 {
		return nil
	}
	return validateNode(args, schema, "arguments")
}

// validateNode walks one schema node. Container nodes recurse into their
// children so nested objects and array items are checked too.
func validateNode(value any, schema map[string]any, path string) error {
	if len(schema) == 0 {
		return nil
	}
	if err := validateEnum(value, schema, path); err != nil {
		return err
	}

	switch schemaType(schema) {
	case "object":
		object, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("%s must be an object", path)
		}
		return validateObject(object, schema, path)
	case "array":
		items, ok := value.([]any)
		if !ok {
			return fmt.Errorf("%s must be an array", path)
		}
		itemSchema, _ := schema["items"].(map[string]any)
		for i, item := range items {
			if err := validateNode(item, itemSchema, fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
	case "string":
		if _, ok := value.(string); !ok {
			return fmt.Errorf("%s must be a string", path)
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("%s must be a boolean", path)
		}
	case "number":
		if _, ok := jsonNumber(value); !ok {
			return fmt.Errorf("%s must be a number", path)
		}
	case "integer":
		number, ok := jsonNumber(value)
		if !ok || math.Trunc(number) != number {
			return fmt.Errorf("%s must be an integer", path)
		}
	case "null":
		if value != nil {
			return fmt.Errorf("%s must be null", path)
		}
	}
	return nil
}

// validateObject enforces required keys, prunes undeclared keys when the schema
// forbids them, and recurses into declared properties.
func validateObject(object map[string]any, schema map[string]any, path string) error {
	for _, name := range requiredFromSchema(schema) {
		if _, ok := object[name]; !ok {
			return fmt.Errorf("%s is missing required argument %q", path, name)
		}
	}

	properties, _ := schema["properties"].(map[string]any)
	if allowed, ok := schema["additionalProperties"].(bool); ok && !allowed && len(properties) > 0 {
		for name := range object {
			if _, declared := properties[name]; !declared {
				delete(object, name)
			}
		}
	}

	for name, child := range object {
		childSchema, ok := properties[name].(map[string]any)
		if !ok {
			continue
		}
		if err := validateNode(child, childSchema, path+"."+name); err != nil {
			return err
		}
	}
	return nil
}

// validateEnum rejects a value the schema does not list. Comparison goes
// through JSON so numbers and strings match the way the caller wrote them.
func validateEnum(value any, schema map[string]any, path string) error {
	allowed, ok := schema["enum"].([]any)
	if !ok || len(allowed) == 0 {
		return nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("%s is not an allowed value", path)
	}
	for _, candidate := range allowed {
		if other, err := json.Marshal(candidate); err == nil && string(other) == string(encoded) {
			return nil
		}
	}
	return fmt.Errorf("%s is not an allowed value", path)
}

// schemaType reads the schema's declared type. A union type is treated as
// unconstrained, because enforcing one branch would reject valid input.
func schemaType(schema map[string]any) string {
	declared, _ := schema["type"].(string)
	return declared
}

// jsonNumber accepts the numeric shapes a decoded JSON document or a
// programmatic definition can produce.
func jsonNumber(value any) (float64, bool) {
	switch number := value.(type) {
	case float64:
		return number, true
	case float32:
		return float64(number), true
	case int:
		return float64(number), true
	case int64:
		return float64(number), true
	case json.Number:
		parsed, err := number.Float64()
		return parsed, err == nil
	}
	return 0, false
}
