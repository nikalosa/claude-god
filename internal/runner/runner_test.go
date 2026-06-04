package runner

import (
	"context"
	"errors"
	"regexp"
	"testing"

	"github.com/nikalosa/claude-god/internal/dsl"
	"github.com/nikalosa/claude-god/internal/judge"
	"github.com/nikalosa/claude-god/internal/parser"
)

var ctx = context.Background()

func recs(texts ...string) []*parser.RunRecord {
	out := make([]*parser.RunRecord, 0, len(texts))
	for _, t := range texts {
		out = append(out, &parser.RunRecord{
			FinalText: t,
			TotalCost: 0.1,
			Usage:     parser.Usage{InputTokens: 100, OutputTokens: 10},
			Timing:    parser.Timing{DurationMs: 1000},
		})
	}
	return out
}

func ruleBasedProbe() dsl.Probe {
	return dsl.Probe{
		ID:     "money",
		Prompt: "how are amounts stored?",
		Kind:   dsl.RuleBased,
		Rules: []dsl.Rule{{ID: "as_string", Severity: dsl.Critical, Checks: []dsl.Check{
			&dsl.TextMatches{Pattern: regexp.MustCompile("(?i)string")},
		}}},
	}
}

func TestGradeProbe_RuleBased(t *testing.T) {
	probe := ruleBasedProbe()
	before := recs("stored as strings", "stored as strings", "stored as strings")
	after := recs("stored as ints", "stored as ints", "stored as strings")

	agg, pref, err := GradeProbe(ctx, probe, before, after, nil)
	if err != nil {
		t.Fatalf("GradeProbe: %v", err)
	}
	if pref != nil {
		t.Errorf("rule-based probe must not produce a preference, got %+v", pref)
	}
	if len(agg.Before.Rules) != 1 || !agg.Before.Rules[0].Pass {
		t.Errorf("before should PASS (3/3): %+v", agg.Before.Rules)
	}
	if len(agg.After.Rules) != 1 || agg.After.Rules[0].Pass {
		t.Errorf("after should FAIL (1/3): %+v", agg.After.Rules)
	}
}

func TestGradeProbe_RuleBased_JudgeRubric(t *testing.T) {
	probe := dsl.Probe{
		ID: "trace", Prompt: "trace it", Kind: dsl.RuleBased,
		Rules: []dsl.Rule{{ID: "r", Severity: dsl.High, Checks: []dsl.Check{
			&dsl.JudgeRubric{Facts: []string{"a", "b"}, PassScore: 60},
		}}},
	}
	j := judge.StubJudge{ScoreValue: 80}
	agg, pref, err := GradeProbe(ctx, probe, recs("x", "x", "x"), recs("y", "y", "y"), j)
	if err != nil {
		t.Fatalf("GradeProbe: %v", err)
	}
	if pref != nil {
		t.Error("rule-based probe must not produce a preference")
	}
	if !agg.Before.Rules[0].Pass || !agg.After.Rules[0].Pass {
		t.Errorf("both envs should PASS at score 80 >= 60: %+v / %+v", agg.Before.Rules, agg.After.Rules)
	}
}

func TestGradeProbe_OpenEnded(t *testing.T) {
	probe := dsl.Probe{ID: "design", Prompt: "tradeoffs?", Kind: dsl.OpenEnded}
	j := judge.StubJudge{Pref: judge.Preference{
		Outcome: judge.AfterBetter, Concise: judge.AfterBetter,
		Exhaustive: judge.Tie, Direct: judge.AfterBetter, Reasoning: "tighter",
	}}
	agg, pref, err := GradeProbe(ctx, probe, recs("a", "a", "a"), recs("b", "b", "b"), j)
	if err != nil {
		t.Fatalf("GradeProbe: %v", err)
	}
	if pref == nil {
		t.Fatal("open-ended probe must produce a preference")
	}
	if pref.ProbeID != "design" || pref.Outcome != judge.AfterBetter {
		t.Errorf("unexpected preference: %+v", pref)
	}
	if len(agg.Before.Rules) != 0 || len(agg.After.Rules) != 0 {
		t.Errorf("open-ended probe must have no rule results")
	}
	// Numbers (medians) must still be aggregated for open-ended probes.
	if agg.Before.MedianCost == 0 || agg.Before.MedianInputTok == 0 {
		t.Errorf("Numbers should be aggregated for open-ended: %+v", agg.Before)
	}
}

func TestGradeProbe_Plan(t *testing.T) {
	probe := dsl.Probe{ID: "rollout", Prompt: "plan it?", Kind: dsl.Plan}
	j := judge.StubJudge{Pref: judge.Preference{
		Outcome: judge.BeforeBetter, Concise: judge.BeforeBetter,
		Exhaustive: judge.Tie, Direct: judge.BeforeBetter, Reasoning: "clearer steps",
	}}
	agg, pref, err := GradeProbe(ctx, probe, recs("a", "a", "a"), recs("b", "b", "b"), j)
	if err != nil {
		t.Fatalf("GradeProbe: %v", err)
	}
	if pref == nil {
		t.Fatal("plan probe must produce a preference")
	}
	if pref.ProbeID != "rollout" || pref.Outcome != judge.BeforeBetter {
		t.Errorf("unexpected preference: %+v", pref)
	}
	if len(agg.Before.Rules) != 0 || len(agg.After.Rules) != 0 {
		t.Errorf("plan probe must have no rule results")
	}
}

func TestGradeProbe_OpenEnded_NilJudgeErrors(t *testing.T) {
	probe := dsl.Probe{ID: "design", Prompt: "q", Kind: dsl.OpenEnded}
	if _, _, err := GradeProbe(ctx, probe, recs("a"), recs("b"), nil); err == nil {
		t.Error("open-ended probe with nil judge should error, not panic")
	}
}

func TestGradeProbe_DegradesOnPreferenceError(t *testing.T) {
	probe := dsl.Probe{ID: "design", Prompt: "q", Kind: dsl.OpenEnded}
	j := judge.StubJudge{PrefErr: errors.New("boom")}
	agg, pref, err := GradeProbe(ctx, probe, recs("a"), recs("b"), j)
	if err != nil {
		t.Fatalf("preference is report-only; a judge failure must not abort grading: %v", err)
	}
	if pref != nil {
		t.Errorf("expected nil preference on judge failure, got %+v", pref)
	}
	if agg.ProbeID != "design" {
		t.Errorf("Numbers must survive a preference failure; ProbeID = %q", agg.ProbeID)
	}
}
