package judge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Backend is the raw LLM call behind the Judge. Hidden behind this interface so
// the claude -p backend can be swapped for the Anthropic API without touching
// callers (ADR-0003).
type Backend interface {
	Ask(ctx context.Context, prompt string) (string, error)
}

type claudeBackend struct{}

// NewClaudeBackend returns the default backend: `claude -p --output-format json`
// run in a fresh empty temp dir (neutral — no target Environment leaks in),
// reusing the developer's OAuth login (no ANTHROPIC_API_KEY required).
//
// Note: the user-level ~/.claude/CLAUDE.md still loads, but it is invariant
// across the Before/After comparison, so it cannot bias a verdict.
func NewClaudeBackend() Backend { return claudeBackend{} }

func (claudeBackend) Ask(ctx context.Context, prompt string) (string, error) {
	// Every judge call is a claude -p, as flake-prone as a run, and it lands in
	// the serial grading phase where a single failure aborts the whole report.
	// Retry transient failures (mirrors the run pool) so grading survives the
	// same turbulence the runs already shrug off.
	const maxAttempts = 3
	var err error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		var out string
		if out, err = askOnce(ctx, prompt); err == nil {
			return out, nil
		}
		if ctx.Err() != nil {
			return "", err
		}
		if attempt < maxAttempts {
			fmt.Fprintf(os.Stderr, "judge retry %d/%d · %v\n", attempt, maxAttempts-1, err)
		}
	}
	return "", err
}

func askOnce(ctx context.Context, prompt string) (string, error) {
	dir, err := os.MkdirTemp("", "claude-benchmark-judge-*")
	if err != nil {
		return "", fmt.Errorf("judge: temp dir: %w", err)
	}
	defer os.RemoveAll(dir)

	// --verbose is required only for stream-json; the json format is a single
	// object and needs no flag (see internal/parser/testdata/stream-json-shape.md).
	cmd := exec.CommandContext(ctx, "claude", "-p", prompt, "--output-format", "json")
	cmd.Dir = dir
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("judge: claude -p: %w: %s", err, strings.TrimSpace(errBuf.String()))
	}
	return parseEnvelope(out.Bytes())
}

// envelope mirrors the terminal result object emitted by
// `claude -p --output-format json` — the single-object analogue of the
// stream-json result event documented in internal/parser/testdata/
// stream-json-shape.md. Only the fields the judge needs are decoded.
type envelope struct {
	Subtype string `json:"subtype"`
	IsError bool   `json:"is_error"`
	Result  string `json:"result"`
}

// parseEnvelope extracts the assistant's final text from a claude -p json
// envelope, returning a typed error on an error envelope or empty result so the
// caller fails loudly rather than grading garbage.
func parseEnvelope(data []byte) (string, error) {
	var e envelope
	if err := json.Unmarshal(data, &e); err != nil {
		return "", fmt.Errorf("judge: parse envelope: %w", err)
	}
	if e.IsError || (e.Subtype != "" && e.Subtype != "success") {
		return "", fmt.Errorf("judge: claude -p error envelope (subtype=%q is_error=%t)", e.Subtype, e.IsError)
	}
	if e.Result == "" {
		return "", fmt.Errorf("judge: claude -p returned empty result")
	}
	return e.Result, nil
}

// extractJSON returns the first balanced top-level JSON object in s, tolerating
// markdown code fences and surrounding prose. The judge is not temperature-0
// (ADR-0003), so its output format wobbles; isolating extraction from the
// scoring math keeps both independently testable.
func extractJSON(s string) ([]byte, error) {
	start := strings.IndexByte(s, '{')
	if start < 0 {
		return nil, fmt.Errorf("no JSON object found")
	}
	depth := 0
	inStr := false
	esc := false
	for i := start; i < len(s); i++ {
		ch := s[i]
		if inStr {
			switch {
			case esc:
				esc = false
			case ch == '\\':
				esc = true
			case ch == '"':
				inStr = false
			}
			continue
		}
		switch ch {
		case '"':
			inStr = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return []byte(s[start : i+1]), nil
			}
		}
	}
	return nil, fmt.Errorf("unbalanced JSON object")
}
