package harness

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

// TestHarness_Dogfood runs the full L1 harness against this repo on `main`.
// Gated behind CLAUDE_VALIDATOR_DOGFOOD=1 because it shells out to a real
// `claude -p` invocation (costs money, takes seconds).
func TestHarness_Dogfood(t *testing.T) {
	if os.Getenv("CLAUDE_VALIDATOR_DOGFOOD") != "1" {
		t.Skip("set CLAUDE_VALIDATOR_DOGFOOD=1 to run")
	}

	target, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	// Walk up to repo root (test runs from internal/harness/).
	for i := 0; i < 5; i++ {
		if _, err := os.Stat(target + "/go.mod"); err == nil {
			break
		}
		target += "/.."
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	res, err := Run(ctx, Opts{
		TargetRepo:    target,
		Branch:        "main",
		Prompt:        "In one sentence, what is the purpose of claude-validator?",
		NoMemSnapshot: true,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if res.Record == nil {
		t.Fatal("nil RunRecord")
	}
	if res.Record.FinalText == "" {
		t.Error("empty FinalText")
	}
	if res.Record.TotalCost <= 0 {
		t.Errorf("expected TotalCost > 0, got %v", res.Record.TotalCost)
	}
	if _, err := os.Stat(res.StreamPath); err != nil {
		t.Errorf("stream artifact missing: %v", err)
	}
	if _, err := os.Stat(res.DiffPath); err != nil {
		t.Errorf("diff artifact missing: %v", err)
	}
	if _, err := os.Stat(res.WorktreePath); err == nil {
		t.Errorf("worktree not cleaned up: %s still exists", res.WorktreePath)
	}

	rj, _ := json.MarshalIndent(res.Record, "", "  ")
	t.Logf("RunRecord:\n%s", rj)
	t.Logf("artifacts: stream=%s diff=%s diff_stat=%s", res.StreamPath, res.DiffPath, res.DiffStatPath)
}

// TestReadOnlyBashSettings pins the shape of the --settings JSON that wires the
// PreToolUse Bash guard (no claude needed).
func TestReadOnlyBashSettings(t *testing.T) {
	s, err := readOnlyBashSettings()
	if err != nil {
		t.Fatalf("readOnlyBashSettings: %v", err)
	}
	var got struct {
		Hooks struct {
			PreToolUse []struct {
				Matcher string `json:"matcher"`
				Hooks   []struct {
					Type    string `json:"type"`
					Command string `json:"command"`
				} `json:"hooks"`
			} `json:"PreToolUse"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal([]byte(s), &got); err != nil {
		t.Fatalf("settings is not valid JSON: %v\n%s", err, s)
	}
	if len(got.Hooks.PreToolUse) != 1 || got.Hooks.PreToolUse[0].Matcher != "Bash" {
		t.Fatalf("expected one PreToolUse hook matching Bash, got %+v", got.Hooks.PreToolUse)
	}
	h := got.Hooks.PreToolUse[0].Hooks
	if len(h) != 1 || h[0].Type != "command" || !strings.HasSuffix(h[0].Command, "__bash-read-guard") {
		t.Fatalf("expected a command hook ending in __bash-read-guard, got %+v", h)
	}
}
