package judge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/nikalosa/claude-god/internal/harness"
)

type Backend interface {
	Ask(ctx context.Context, prompt string) (string, error)
}

type claudeBackend struct{}

func NewClaudeBackend() Backend { return claudeBackend{} }

func (claudeBackend) Ask(ctx context.Context, prompt string) (string, error) {

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

	cmd := exec.CommandContext(ctx, "claude", "-p", prompt, "--output-format", "json")
	cmd.Dir = dir
	cmd.Env = harness.SanitizedEnv(os.Environ())
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("judge: claude -p: %w: %s", err, strings.TrimSpace(errBuf.String()))
	}
	return parseEnvelope(out.Bytes())
}

type envelope struct {
	Subtype string `json:"subtype"`
	IsError bool   `json:"is_error"`
	Result  string `json:"result"`
}

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
