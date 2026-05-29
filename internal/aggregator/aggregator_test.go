package aggregator

import (
	"math"
	"testing"

	"github.com/nikalosa/claude-god/internal/dsl"
	"github.com/nikalosa/claude-god/internal/parser"
)

func nearly(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func mkRun(cost float64, in, out, dur int, results ...dsl.RuleResult) Run {
	return Run{
		Record: &parser.RunRecord{
			TotalCost: cost,
			Usage:     parser.Usage{InputTokens: in, OutputTokens: out},
			Timing:    parser.Timing{DurationMs: dur},
		},
		Results: results,
	}
}

func TestMedian_Odd(t *testing.T) {
	if median([]float64{3, 1, 2}) != 2 {
		t.Error("expected median 2")
	}
	if medianInt([]int{5, 1, 3, 2, 4}) != 3 {
		t.Error("expected median 3")
	}
}

func TestMedian_Even(t *testing.T) {
	if median([]float64{1, 2, 3, 4}) != 2.5 {
		t.Error("expected median 2.5")
	}
	if medianInt([]int{10, 20}) != 15 {
		t.Error("expected median 15")
	}
}

func TestMedian_Empty(t *testing.T) {
	if median(nil) != 0 || medianInt(nil) != 0 {
		t.Error("expected 0 for empty")
	}
}

func TestAggregate_MajorityVoteAndDisagreement(t *testing.T) {
	res := func(pass bool) dsl.RuleResult {
		return dsl.RuleResult{RuleID: "r1", Severity: dsl.Critical, Pass: pass}
	}
	po := ProbeOutcome{
		ProbeID: "p1",
		Before: EnvOutcome{Runs: []Run{
			mkRun(0.10, 1000, 50, 2000, res(true)),
			mkRun(0.11, 1100, 60, 2200, res(true)),
			mkRun(0.09, 900, 40, 1800, res(false)),
		}},
		After: EnvOutcome{Runs: []Run{
			mkRun(0.05, 500, 20, 1000, res(false)),
			mkRun(0.06, 600, 25, 1100, res(false)),
			mkRun(0.04, 400, 15, 900, res(true)),
		}},
	}
	agg := Aggregate(po)

	// Before: 2/3 PASS → PASS, disagreement
	if !agg.Before.Rules[0].Pass {
		t.Errorf("before majority should be PASS, got %+v", agg.Before.Rules[0])
	}
	if agg.Before.Rules[0].PassCount != 2 || agg.Before.Rules[0].Total != 3 || !agg.Before.Rules[0].Disagreement {
		t.Errorf("before votes: %+v", agg.Before.Rules[0])
	}
	// After: 1/3 PASS → FAIL, disagreement
	if agg.After.Rules[0].Pass {
		t.Errorf("after majority should be FAIL, got %+v", agg.After.Rules[0])
	}
	if agg.After.Rules[0].PassCount != 1 || !agg.After.Rules[0].Disagreement {
		t.Errorf("after votes: %+v", agg.After.Rules[0])
	}
	// Medians
	if !nearly(agg.Before.MedianCost, 0.10) {
		t.Errorf("before median cost = %v", agg.Before.MedianCost)
	}
	if agg.Before.MedianInputTok != 1000 || agg.Before.MedianOutputTok != 50 || agg.Before.MedianDurationMs != 2000 {
		t.Errorf("before medians: %+v", agg.Before)
	}
}

func TestAggregate_Unanimous(t *testing.T) {
	res := dsl.RuleResult{RuleID: "r1", Severity: dsl.Critical, Pass: true}
	po := ProbeOutcome{
		Before: EnvOutcome{Runs: []Run{
			mkRun(0.1, 100, 10, 1000, res),
			mkRun(0.1, 100, 10, 1000, res),
			mkRun(0.1, 100, 10, 1000, res),
		}},
	}
	agg := Aggregate(po)
	if !agg.Before.Rules[0].Pass {
		t.Error("unanimous PASS should be PASS")
	}
	if agg.Before.Rules[0].Disagreement {
		t.Error("unanimous should not be disagreement")
	}
	if agg.Before.Rules[0].PassCount != 3 || agg.Before.Rules[0].Total != 3 {
		t.Errorf("votes: %+v", agg.Before.Rules[0])
	}
}

func TestCompare_FromAggregated(t *testing.T) {
	aggs := []AggregatedOutcome{{
		ProbeID: "p1",
		Before: AggregatedEnv{Rules: []AggregatedRuleResult{
			{RuleID: "regress", Severity: dsl.Critical, Pass: true, PassCount: 3, Total: 3},
			{RuleID: "newpass", Severity: dsl.High, Pass: false, PassCount: 0, Total: 3},
		}},
		After: AggregatedEnv{Rules: []AggregatedRuleResult{
			{RuleID: "regress", Severity: dsl.Critical, Pass: false, PassCount: 0, Total: 3},
			{RuleID: "newpass", Severity: dsl.High, Pass: true, PassCount: 3, Total: 3},
		}},
	}}
	verdicts := Compare(aggs)
	statuses := map[string]Status{}
	for _, v := range verdicts {
		statuses[v.RuleID] = v.Status
	}
	if statuses["regress"] != Regression {
		t.Errorf("expected Regression, got %s", statuses["regress"])
	}
	if statuses["newpass"] != NewPass {
		t.Errorf("expected NewPass, got %s", statuses["newpass"])
	}
}

func TestComputeDeltas_Medians(t *testing.T) {
	aggs := []AggregatedOutcome{
		{
			Before: AggregatedEnv{MedianCost: 0.10, MedianInputTok: 1000, MedianOutputTok: 50, MedianDurationMs: 2000},
			After:  AggregatedEnv{MedianCost: 0.07, MedianInputTok: 700, MedianOutputTok: 40, MedianDurationMs: 1500},
		},
		{
			Before: AggregatedEnv{MedianCost: 0.05, MedianInputTok: 500, MedianOutputTok: 20, MedianDurationMs: 1000},
			After:  AggregatedEnv{MedianCost: 0.04, MedianInputTok: 400, MedianOutputTok: 15, MedianDurationMs: 800},
		},
	}
	d := ComputeDeltas(aggs)
	if !nearly(d.CostBefore, 0.15) || !nearly(d.CostAfter, 0.11) {
		t.Errorf("cost: %+v", d)
	}
	if d.InputTokBefore != 1500 || d.InputTokAfter != 1100 {
		t.Errorf("input tokens: %+v", d)
	}
}

func TestHasCriticalRegression(t *testing.T) {
	if HasCriticalRegression([]Verdict{
		{Severity: dsl.High, Status: Regression},
		{Severity: dsl.Critical, Status: Stable},
	}) {
		t.Error("no critical regression should be false")
	}
	if !HasCriticalRegression([]Verdict{
		{Severity: dsl.Critical, Status: Regression},
	}) {
		t.Error("critical regression should be true")
	}
}
