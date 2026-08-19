package toolcalling

import (
	"strings"
	"testing"
)

func grammarTestTools() []ToolDef {
	return []ToolDef{
		{Type: "custom", Name: "exec"},
		{Type: "function", Name: "get_weather"},
	}
}

func TestGrammarToolNamesPicksOnlyCustomTools(t *testing.T) {
	names := GrammarToolNames(grammarTestTools())
	if len(names) != 1 || names[0] != "exec" {
		t.Fatalf("names = %#v, want only the custom tool", names)
	}
}

func TestBridgeEnvelopeBodyClaimsALoneEnvelope(t *testing.T) {
	body, ok := BridgeEnvelopeBody(`{"input":"shell({cmd:\"cat README.md\"}); text(plan);"}`)
	if !ok {
		t.Fatal("a lone bridge envelope was not claimed")
	}
	if !strings.Contains(body, "cat README.md") {
		t.Fatalf("body = %q, want the unwrapped program", body)
	}
}

func TestBridgeEnvelopeBodyIgnoresEmbeddedAndExtendedObjects(t *testing.T) {
	cases := []string{
		`Here is the payload: {"input":"shell();"} and that is all.`,
		`{"input":"shell();","note":"extra"}`,
		`{"other":"shell();"}`,
		`{"input":""}`,
		`not json at all`,
	}
	for _, text := range cases {
		if _, ok := BridgeEnvelopeBody(text); ok {
			t.Fatalf("claimed a text that is not a lone envelope: %q", text)
		}
	}
}

func TestCodeModeSourceCallClaimsABareProgram(t *testing.T) {
	body, ok := CodeModeSourceCall("shell({cmd:\"cat README.md\"});\ntext(plan);")
	if !ok {
		t.Fatal("a bare code-mode program was not claimed")
	}
	if !strings.HasPrefix(body, "shell(") {
		t.Fatalf("body = %q, want the program itself", body)
	}
}

func TestCodeModeSourceCallLeavesProseAlone(t *testing.T) {
	cases := []string{
		"You can run shell commands to inspect the repository.",
		"Run this:\n```js\nshell({cmd:\"ls\"});\n```",
		"The shell(cmd) helper reads a command and returns its output.",
		"",
	}
	for _, text := range cases {
		if _, ok := CodeModeSourceCall(text); ok {
			t.Fatalf("prose was claimed as a program: %q", text)
		}
	}
}

func TestGrammarBodyCallNeedsADeclaredGrammarTool(t *testing.T) {
	onlyFunctions := []ToolDef{{Type: "function", Name: "get_weather"}}
	if _, ok := GrammarBodyCall(`{"input":"shell();"}`, onlyFunctions, nil); ok {
		t.Fatal("a body was claimed without any custom tool declared")
	}
}

func TestGrammarBodyCallCarriesTheRawBody(t *testing.T) {
	call, ok := GrammarBodyCall(`{"input":"shell({cmd:\"ls\"});"}`, grammarTestTools(), nil)
	if !ok {
		t.Fatal("the bridge envelope was not claimed")
	}
	if call.Name != "exec" {
		t.Fatalf("name = %q, want the declared custom tool", call.Name)
	}
	// A custom tool item puts the arguments straight into its `input` field,
	// so the body must arrive raw rather than re-wrapped.
	if string(call.Arguments) != `shell({cmd:"ls"});` {
		t.Fatalf("arguments = %q, want the unwrapped body", string(call.Arguments))
	}
	if call.ID == "" {
		t.Fatal("the claimed call carries no id")
	}
}

func TestGrammarBodyCallHonorsTheAllowFilter(t *testing.T) {
	denyAll := func(string) bool { return false }
	if _, ok := GrammarBodyCall(`{"input":"shell();"}`, grammarTestTools(), denyAll); ok {
		t.Fatal("a body was claimed for a tool the policy disallows")
	}
}
