package cli

import (
	"context"
	"reflect"
	"testing"

	"github.com/nikalosa/claude-god/internal/dsl"
)

func TestJudgeForAssess(t *testing.T) {
	comparative := []dsl.Probe{{ID: "design", Prompt: "x", Kind: dsl.OpenEnded}}
	if j, err := judgeForAssess(comparative, false); err != nil || j != nil {
		t.Errorf("comparative-only corpus must need no judge without --judge: j=%v err=%v", j, err)
	}

	rubric := []dsl.Probe{{ID: "r", Prompt: "q", Kind: dsl.RuleBased, Rules: []dsl.Rule{{
		ID: "fact", Severity: dsl.Critical, Checks: []dsl.Check{&dsl.JudgeRubric{Facts: []string{"f"}, PassScore: 50}},
	}}}}
	if _, err := judgeForAssess(rubric, false); err == nil {
		t.Error("judge_rubric corpus must require --judge")
	}
	if _, err := judgeForAssess(rubric, true); err != nil {
		t.Errorf("--judge should satisfy a judge_rubric corpus: %v", err)
	}
}

func TestRunSingleEnv(t *testing.T) {
	probes := append(poolTestProbes(), dsl.Probe{ID: "design", Prompt: "design", Kind: dsl.OpenEnded})
	ctx := context.Background()

	aggs, err := runSingleEnv(ctx, probes, Env{Ref: "before"}, 1, 4, fakeRun, tc(t), false, nil)
	if err != nil {
		t.Fatalf("runSingleEnv: %v", err)
	}
	if len(aggs) != 3 {
		t.Fatalf("want 3 aggregates, got %d", len(aggs))
	}

	byID := map[string]int{}
	for i, a := range aggs {
		byID[a.ProbeID] = i
	}

	a := aggs[byID["A"]].Before
	if len(a.Rules) != 1 || !a.Rules[0].Pass {
		t.Errorf("probe A should pass single-env: %+v", a.Rules)
	}
	if a.MedianCost == 0 {
		t.Error("single-env Numbers should be populated")
	}
	if b := aggs[byID["B"]].Before; len(b.Rules) != 1 || b.Rules[0].Pass {
		t.Errorf("probe B should fail single-env: %+v", b.Rules)
	}

	if d := aggs[byID["design"]].Before; len(d.Rules) != 0 || d.MedianCost == 0 {
		t.Errorf("comparative probe should have no rules but real Numbers: %+v", d)
	}

	if aggs[byID["A"]].After.MedianCost != 0 {
		t.Error("assess must not run a second Environment")
	}
}

func TestRunSingleEnv_DeterministicAcrossConcurrency(t *testing.T) {
	probes := poolTestProbes()
	ctx := context.Background()

	a1, err := runSingleEnv(ctx, probes, Env{Ref: "before"}, 3, 1, fakeRun, tc(t), false, nil)
	if err != nil {
		t.Fatalf("concurrency 1: %v", err)
	}
	a8, err := runSingleEnv(ctx, probes, Env{Ref: "before"}, 3, 8, fakeRun, tc(t), false, nil)
	if err != nil {
		t.Fatalf("concurrency 8: %v", err)
	}
	if !reflect.DeepEqual(a1, a8) {
		t.Errorf("single-env aggregates differ by concurrency:\n c1=%+v\n c8=%+v", a1, a8)
	}
}
