// Package cache persists completed RunRecords keyed by an environment
// Fingerprint so an unchanged environment is never re-run (ADR-0016). The cached
// unit is the raw Run; grading is always re-done from the stored record, so
// editing a Rule, Severity, Check, or the Judge costs no run.
package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// Inputs is the complete, resolved key material for one (environment, probe)
// pair. Every field is keyed: dropping any one would serve a stale run for a
// changed environment — a false hit, the one failure that corrupts a fidelity
// tool. Excludes everything that is re-graded (Rules/Severities/Checks/Judge).
type Inputs struct {
	CommitSHA  string // ref resolved to a SHA (branch is a moving pointer)
	MCPConfig  string // effective MCP config bytes ("" = no MCP layer); normalized before hashing
	MemTag     string // memory policy: "snapshot" | "none" | "live:<hash>"
	Model      string
	Effort     string
	CLIVersion string
	RunPrompt  string // the prompt actually sent to claude (Plan mode is baked in via the wrap)
}

// Fingerprint is the Run cache key: a deterministic 64-char hex SHA-256 of the
// inputs. It marshals a fixed-field struct (no maps), so field order is stable
// and boundaries are unambiguous — "ab"+"c" cannot collide with "a"+"bc".
func Fingerprint(in Inputs) string {
	in.MCPConfig = normalizeJSON(in.MCPConfig)
	b, err := json.Marshal(in)
	if err != nil {
		// Inputs is all strings — Marshal cannot fail; fall back defensively.
		b = []byte(in.CommitSHA + "\x00" + in.MCPConfig + "\x00" + in.MemTag + "\x00" +
			in.Model + "\x00" + in.Effort + "\x00" + in.CLIVersion + "\x00" + in.RunPrompt)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// normalizeJSON canonicalizes valid JSON (json.Marshal sorts map keys and drops
// insignificant whitespace) so a reformatted-but-equal MCP config hits the same
// pool. Non-JSON (or empty) passes through unchanged.
func normalizeJSON(s string) string {
	if s == "" {
		return ""
	}
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return s
	}
	b, err := json.Marshal(v)
	if err != nil {
		return s
	}
	return string(b)
}
