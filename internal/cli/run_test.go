package cli

import (
	"strings"
	"testing"

	"github.com/nikalosa/claude-god/internal/dsl"
)

func TestParseLevels(t *testing.T) {
	cases := []struct {
		in      string
		want    []string
		wantErr bool
	}{
		{"l1", []string{"l1"}, false},
		{"l2", []string{"l2"}, false},
		{"l1,l2", []string{"l1", "l2"}, false},
		{" l1 , l2 ", []string{"l1", "l2"}, false},
		{"l2,l2", []string{"l2"}, false},
		{"l3", nil, true},
		{"l1,l3", nil, true},
		{"l4", nil, true},
		{"l1,l4", nil, true},
		{"bogus", nil, true},
		{"", nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			set, err := parseLevels(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Errorf("parseLevels(%q): expected error", tc.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseLevels(%q): %v", tc.in, err)
			}
			for _, w := range tc.want {
				if !set[w] {
					t.Errorf("parseLevels(%q) missing %q", tc.in, w)
				}
			}
			if len(set) != len(tc.want) {
				t.Errorf("parseLevels(%q) = %v, want exactly %v", tc.in, set, tc.want)
			}
		})
	}
}

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
// judge_rubric rules) needs the judge, so l2 satisfies it and l1 errors.
func TestJudgeFor_Comparative(t *testing.T) {
	probes := []dsl.Probe{{ID: "p", Prompt: "x", Kind: dsl.Plan}}
	if _, err := judgeFor(probes, map[string]bool{"l2": true}); err != nil {
		t.Errorf("l2 should satisfy the judge requirement: %v", err)
	}
	if _, err := judgeFor(probes, map[string]bool{"l1": true}); err == nil {
		t.Error("expected error when a comparative corpus lacks l2")
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
