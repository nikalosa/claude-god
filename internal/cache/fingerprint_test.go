package cache

import "testing"

func baseInputs() Inputs {
	return Inputs{
		CommitSHA:  "abc123",
		MCPConfig:  `{"mcpServers":{"cg":{"command":"x"}}}`,
		MemTag:     "snapshot",
		Model:      "claude-opus-4-8",
		Effort:     "medium",
		CLIVersion: "2.1.195",
		RunPrompt:  "How are monetary amounts typed?",
	}
}

func TestFingerprint_Deterministic(t *testing.T) {
	a := Fingerprint(baseInputs())
	b := Fingerprint(baseInputs())
	if a != b {
		t.Fatalf("non-deterministic: %q != %q", a, b)
	}
	if len(a) != 64 {
		t.Errorf("want 64-char hex digest, got %d chars: %q", len(a), a)
	}
}

func TestFingerprint_EveryFieldIsKeyed(t *testing.T) {
	base := Fingerprint(baseInputs())
	mutate := map[string]func(*Inputs){
		"CommitSHA":  func(i *Inputs) { i.CommitSHA = "def456" },
		"MCPConfig":  func(i *Inputs) { i.MCPConfig = `{"mcpServers":{"other":{"command":"y"}}}` },
		"MemTag":     func(i *Inputs) { i.MemTag = "none" },
		"Model":      func(i *Inputs) { i.Model = "claude-sonnet-4-6" },
		"Effort":     func(i *Inputs) { i.Effort = "high" },
		"CLIVersion": func(i *Inputs) { i.CLIVersion = "2.1.196" },
		"RunPrompt":  func(i *Inputs) { i.RunPrompt = "different prompt" },
	}
	for field, m := range mutate {
		in := baseInputs()
		m(&in)
		if got := Fingerprint(in); got == base {
			t.Errorf("%s is not keyed: mutating it left the digest unchanged", field)
		}
	}
}

func TestFingerprint_NoConcatAmbiguity(t *testing.T) {
	x := baseInputs()
	x.Model, x.Effort = "ab", "c"
	y := baseInputs()
	y.Model, y.Effort = "a", "bc"
	if Fingerprint(x) == Fingerprint(y) {
		t.Error("field boundary is ambiguous: distinct (Model,Effort) splits collided")
	}
}

func TestFingerprint_MCPNormalized(t *testing.T) {
	compact := baseInputs()
	compact.MCPConfig = `{"mcpServers":{"cg":{"command":"x"}}}`
	pretty := baseInputs()
	pretty.MCPConfig = "{\n  \"mcpServers\": {\n    \"cg\": { \"command\": \"x\" }\n  }\n}"
	if Fingerprint(compact) != Fingerprint(pretty) {
		t.Error("semantically-equal MCP configs must share a pool (normalize before hashing)")
	}

	changed := baseInputs()
	changed.MCPConfig = `{"mcpServers":{"cg":{"command":"DIFFERENT"}}}`
	if Fingerprint(compact) == Fingerprint(changed) {
		t.Error("a real MCP change must miss")
	}
}

func TestFingerprint_EmptyMCP(t *testing.T) {
	none := baseInputs()
	none.MCPConfig = ""
	if Fingerprint(none) == Fingerprint(baseInputs()) {
		t.Error("empty MCP must differ from a declared MCP")
	}
	other := none
	if Fingerprint(none) != Fingerprint(other) {
		t.Error("empty MCP must still be deterministic")
	}
}
