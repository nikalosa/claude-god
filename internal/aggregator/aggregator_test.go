package aggregator

import (
	"math"
	"testing"

	"github.com/nikalosa/claude-god/internal/dsl"
	"github.com/nikalosa/claude-god/internal/parser"
)

func nearly(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func mkRun(cost float64, in, out, dur int, results ...dsl.RuleResult) Run {
	return mkRunTools(cost, in, out, dur, 0, results...)
}

func mkRunTools(cost float64, in, out, dur, tools int, results ...dsl.RuleResult) Run {
	calls := make([]parser.ToolCall, tools)
	return Run{
		Record: &parser.RunRecord{
			TotalCost: cost,
			Usage:     parser.Usage{InputTokens: in, OutputTokens: out},
			Timing:    parser.Timing{DurationMs: dur},
			ToolCalls: calls,
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
	if !nearly(agg.Before.MedianCost, 0.10) {
		t.Errorf("before median cost = %v", agg.Before.MedianCost)
	}
	if agg.Before.MedianInputTok != 1000 || agg.Before.MedianOutputTok != 50 || agg.Before.MedianDurationMs != 2000 {
		t.Errorf("before medians: %+v", agg.Before)
	}
}

func TestAggregate_MedianToolCalls(t *testing.T) {
	// Before thrashes (many tool calls), after answers directly (few). The
	// CodeGraph-style efficiency signal: the leaner env makes fewer tool calls.
	po := ProbeOutcome{
		Before: EnvOutcome{Runs: []Run{
			mkRunTools(0.1, 100, 10, 1000, 10),
			mkRunTools(0.1, 100, 10, 1000, 12),
			mkRunTools(0.1, 100, 10, 1000, 14),
		}},
		After: EnvOutcome{Runs: []Run{
			mkRunTools(0.1, 100, 10, 1000, 1),
			mkRunTools(0.1, 100, 10, 1000, 2),
			mkRunTools(0.1, 100, 10, 1000, 3),
		}},
	}
	agg := Aggregate(po)
	if agg.Before.MedianToolCalls != 12 {
		t.Errorf("before median tool calls = %d, want 12", agg.Before.MedianToolCalls)
	}
	if agg.After.MedianToolCalls != 2 {
		t.Errorf("after median tool calls = %d, want 2", agg.After.MedianToolCalls)
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
			Before: AggregatedEnv{MedianCost: 0.10, MedianInputTok: 1000, MedianOutputTok: 50, MedianDurationMs: 2000, MedianToolCalls: 12},
			After:  AggregatedEnv{MedianCost: 0.07, MedianInputTok: 700, MedianOutputTok: 40, MedianDurationMs: 1500, MedianToolCalls: 2},
		},
		{
			Before: AggregatedEnv{MedianCost: 0.05, MedianInputTok: 500, MedianOutputTok: 20, MedianDurationMs: 1000, MedianToolCalls: 5},
			After:  AggregatedEnv{MedianCost: 0.04, MedianInputTok: 400, MedianOutputTok: 15, MedianDurationMs: 800, MedianToolCalls: 1},
		},
	}
	d := ComputeDeltas(aggs)
	if !nearly(d.CostBefore, 0.15) || !nearly(d.CostAfter, 0.11) {
		t.Errorf("cost: %+v", d)
	}
	if d.InputTokBefore != 1500 || d.InputTokAfter != 1100 {
		t.Errorf("input tokens: %+v", d)
	}
	if d.ToolCallsBefore != 17 || d.ToolCallsAfter != 3 {
		t.Errorf("tool calls: before=%d after=%d, want 17/3", d.ToolCallsBefore, d.ToolCallsAfter)
	}
}

func TestFlaky(t *testing.T) {
	mk := func(id string, status Status, beforeDis, afterDis bool) Verdict {
		return Verdict{
			RuleID: id, Status: status,
			Before: VerdictSide{Disagreement: beforeDis},
			After:  VerdictSide{Disagreement: afterDis},
		}
	}
	vs := []Verdict{
		mk("clean_pass", Stable, false, false),
		mk("clean_fail", StableFail, false, false),
		mk("flipped", Regression, false, false),
		mk("newpass", NewPass, false, false),
		mk("disagreed", Stable, true, false),
	}
	flaky := map[string]bool{}
	for _, v := range Flaky(vs) {
		flaky[v.RuleID] = true
	}
	if flaky["clean_pass"] || flaky["clean_fail"] {
		t.Error("clean stable rules must not be flaky")
	}
	for _, id := range []string{"flipped", "newpass", "disagreed"} {
		if !flaky[id] {
			t.Errorf("%s should be flaky", id)
		}
	}
	if len(flaky) != 3 {
		t.Errorf("expected 3 flaky rules, got %d", len(flaky))
	}
}

func TestAggregate_InputOutputFromModelUsage(t *testing.T) {
	// result.usage reports only the final turn; the true session total lives in
	// modelUsage. Aggregation must sum modelUsage, not result.usage.
	run := func() Run {
		return Run{Record: &parser.RunRecord{
			// Final-turn snapshot — must be IGNORED by the metric.
			Usage: parser.Usage{InputTokens: 6000, OutputTokens: 200, CacheCreationInputTokens: 1000, CacheReadInputTokens: 3000},
			ModelUsage: map[string]parser.ModelUsage{
				"opus":  {InputTokens: 10000, OutputTokens: 1000, CacheCreationInputTokens: 2000, CacheReadInputTokens: 50000},
				"haiku": {InputTokens: 500, OutputTokens: 50},
			},
		}}
	}
	po := ProbeOutcome{Before: EnvOutcome{Runs: []Run{run(), run(), run()}}}
	agg := Aggregate(po)
	// input = (10000+2000+50000) + 500 = 62500 (NOT result.usage's 10000)
	if agg.Before.MedianInputTok != 62500 {
		t.Errorf("input median = %d, want 62500 (modelUsage aggregate)", agg.Before.MedianInputTok)
	}
	if agg.Before.MedianOutputTok != 1050 {
		t.Errorf("output median = %d, want 1050", agg.Before.MedianOutputTok)
	}
}
