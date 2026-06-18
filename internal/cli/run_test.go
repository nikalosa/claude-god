package cli

import (
	"strings"
	"testing"

	"github.com/nikalosa/claude-god/internal/dsl"
)

func TestTaskPrompt(t *testing.T) {
	plan := dsl.Probe{ID: "p", Prompt: "Add caching.", Kind: dsl.Plan}
	got := taskPrompt(plan)
	if !strings.Contains(got, "step-by-step plan") || !strings.Contains(got, "Add caching.") {
		t.Errorf("plan prompt should ask for a plan and include the task: %q", got)
	}
	for _, k := range []dsl.ProbeKind{dsl.OpenEnded, dsl.RuleBased} {
		p := dsl.Probe{ID: "p", Prompt: "Add caching.", Kind: k}
		if taskPrompt(p) != "Add caching." {
			t.Errorf("%s prompt must pass through unchanged: %q", k, taskPrompt(p))
		}
	}
}

// TestJudgeFor_Comparative pins the gotcha: a plan corpus (NeedsJudge=true, no
// judge_rubric rules) needs the judge, so --judge satisfies it and the default
// (off) errors.
func TestJudgeFor_Comparative(t *testing.T) {
	probes := []dsl.Probe{{ID: "p", Prompt: "x", Kind: dsl.Plan}}
	if _, err := judgeFor(probes, true); err != nil {
		t.Errorf("--judge should satisfy the judge requirement: %v", err)
	}
	if _, err := judgeFor(probes, false); err == nil {
		t.Error("expected error when a comparative corpus runs without --judge")
	}
}

func TestValidateSamples(t *testing.T) {
	for _, n := range []int{1, 3, 5, 7} {
		if err := validateSamples(n); err != nil {
			t.Errorf("validateSamples(%d): unexpected error %v", n, err)
		}
	}
	for _, n := range []int{0, -1, 2, 4} {
		if err := validateSamples(n); err == nil {
			t.Errorf("validateSamples(%d): expected error", n)
		}
	}
}
