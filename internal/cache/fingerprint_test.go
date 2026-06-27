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

// TestFingerprint_Deterministic: the key is a pure function of its inputs — same
// inputs must hash to the same 64-char hex digest every call.
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

// TestFingerprint_EveryFieldIsKeyed is the false-hit guard: changing ANY keyed
// input must change the digest. A field silently dropped from the key would serve
// a stale run for a changed environment — the one failure that corrupts a fidelity
// tool (ADR-0016 landmine).
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

// TestFingerprint_NoConcatAmbiguity: fields must be unambiguously delimited, so
// moving a character across a field boundary changes the digest (else "ab"+"c"
// and "a"+"bc" would collide into one pool).
func TestFingerprint_NoConcatAmbiguity(t *testing.T) {
	x := baseInputs()
	x.Model, x.Effort = "ab", "c"
	y := baseInputs()
	y.Model, y.Effort = "a", "bc"
	if Fingerprint(x) == Fingerprint(y) {
		t.Error("field boundary is ambiguous: distinct (Model,Effort) splits collided")
	}
}

// TestFingerprint_MCPNormalized: an effective MCP config that differs only in
// whitespace/key-order is the same environment, so it must hit the same pool —
// reformatting .mcp.json must not force a multi-hour re-run. A genuine change
// (different server) must still miss.
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

// TestFingerprint_EmptyMCP: no MCP layer is a valid, stable environment; it must
// hash deterministically and differ from any non-empty config.
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
