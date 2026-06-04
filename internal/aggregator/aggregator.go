package aggregator

import (
	"sort"

	"github.com/nikalosa/claude-god/internal/dsl"
	"github.com/nikalosa/claude-god/internal/parser"
)

type Run struct {
	Record  *parser.RunRecord
	Results []dsl.RuleResult
}

type EnvOutcome struct {
	Runs []Run
}

type ProbeOutcome struct {
	ProbeID string
	Before  EnvOutcome
	After   EnvOutcome
}

type AggregatedRuleResult struct {
	RuleID       string
	Severity     dsl.Severity
	Pass         bool
	PassCount    int
	Total        int
	Disagreement bool
}

type AggregatedEnv struct {
	MedianCost       float64
	MedianInputTok   int
	MedianOutputTok  int
	MedianDurationMs int
	MedianToolCalls  int
	Rules            []AggregatedRuleResult
}

type AggregatedOutcome struct {
	ProbeID string
	Before  AggregatedEnv
	After   AggregatedEnv
}

// Aggregate collapses an EnvOutcome's N runs into one AggregatedEnv: median
// over cost/tokens/duration, majority-vote (>=ceil(N/2)) per rule.
//
// AdaptiveExpansionSeam: PRD prescribes expanding to N=5 when a critical-severity
// rule shows post-N=3 disagreement (PassCount ∈ {1,2}). This is explicitly
// deferred from v1 — the caller currently never re-samples. To enable, the run
// loop should detect a critical Disagreement here and dispatch two additional
// harness runs, appending to EnvOutcome.Runs before re-calling Aggregate.
func Aggregate(po ProbeOutcome) AggregatedOutcome {
	return AggregatedOutcome{
		ProbeID: po.ProbeID,
		Before:  aggregateEnv(po.Before),
		After:   aggregateEnv(po.After),
	}
}

func aggregateEnv(e EnvOutcome) AggregatedEnv {
	costs := make([]float64, 0, len(e.Runs))
	inputs := make([]int, 0, len(e.Runs))
	outputs := make([]int, 0, len(e.Runs))
	durations := make([]int, 0, len(e.Runs))
	toolCalls := make([]int, 0, len(e.Runs))
	for _, r := range e.Runs {
		if r.Record == nil {
			continue
		}
		costs = append(costs, r.Record.TotalCost)
		inputs = append(inputs, r.Record.TotalInputTokens())
		outputs = append(outputs, r.Record.TotalOutputTokens())
		durations = append(durations, r.Record.Timing.DurationMs)
		toolCalls = append(toolCalls, len(r.Record.ToolCalls))
	}

	perRule := map[string][]dsl.RuleResult{}
	var order []string
	seen := map[string]bool{}
	for _, r := range e.Runs {
		for _, rr := range r.Results {
			if !seen[rr.RuleID] {
				seen[rr.RuleID] = true
				order = append(order, rr.RuleID)
			}
			perRule[rr.RuleID] = append(perRule[rr.RuleID], rr)
		}
	}

	var aggRules []AggregatedRuleResult
	for _, id := range order {
		votes := perRule[id]
		passes := 0
		for _, v := range votes {
			if v.Pass {
				passes++
			}
		}
		total := len(votes)
		threshold := (total + 1) / 2
		aggRules = append(aggRules, AggregatedRuleResult{
			RuleID:       id,
			Severity:     votes[0].Severity,
			Pass:         passes >= threshold,
			PassCount:    passes,
			Total:        total,
			Disagreement: passes != 0 && passes != total,
		})
	}

	return AggregatedEnv{
		MedianCost:       median(costs),
		MedianInputTok:   medianInt(inputs),
		MedianOutputTok:  medianInt(outputs),
		MedianDurationMs: medianInt(durations),
		MedianToolCalls:  medianInt(toolCalls),
		Rules:            aggRules,
	}
}

type Status int

const (
	Stable Status = iota
	StableFail
	Regression
	NewPass
)

func (s Status) String() string {
	switch s {
	case Stable:
		return "stable"
	case StableFail:
		return "stable_fail"
	case Regression:
		return "regression"
	case NewPass:
		return "new_pass"
	}
	return "unknown"
}

type VerdictSide struct {
	Pass         bool
	PassCount    int
	Total        int
	Disagreement bool
}

type Verdict struct {
	ProbeID  string
	RuleID   string
	Severity dsl.Severity
	Before   VerdictSide
	After    VerdictSide
	Status   Status
}

func Compare(aggs []AggregatedOutcome) []Verdict {
	var out []Verdict
	for _, ao := range aggs {
		beforeByID := indexAggRules(ao.Before.Rules)
		afterByID := indexAggRules(ao.After.Rules)
		for id, br := range beforeByID {
			ar, ok := afterByID[id]
			if !ok {
				continue
			}
			out = append(out, Verdict{
				ProbeID:  ao.ProbeID,
				RuleID:   id,
				Severity: br.Severity,
				Before:   sideFrom(br),
				After:    sideFrom(ar),
				Status:   classify(br.Pass, ar.Pass),
			})
		}
	}
	return out
}

func sideFrom(r AggregatedRuleResult) VerdictSide {
	return VerdictSide{Pass: r.Pass, PassCount: r.PassCount, Total: r.Total, Disagreement: r.Disagreement}
}

func indexAggRules(rs []AggregatedRuleResult) map[string]AggregatedRuleResult {
	m := make(map[string]AggregatedRuleResult, len(rs))
	for _, r := range rs {
		m[r.RuleID] = r
	}
	return m
}

func classify(before, after bool) Status {
	switch {
	case before && after:
		return Stable
	case !before && !after:
		return StableFail
	case before && !after:
		return Regression
	default:
		return NewPass
	}
}

type Deltas struct {
	CostBefore       float64
	CostAfter        float64
	InputTokBefore   int
	InputTokAfter    int
	OutputTokBefore  int
	OutputTokAfter   int
	DurationMsBefore int
	DurationMsAfter  int
	ToolCallsBefore  int
	ToolCallsAfter   int
}

func ComputeDeltas(aggs []AggregatedOutcome) Deltas {
	var d Deltas
	for _, a := range aggs {
		d.CostBefore += a.Before.MedianCost
		d.CostAfter += a.After.MedianCost
		d.InputTokBefore += a.Before.MedianInputTok
		d.InputTokAfter += a.After.MedianInputTok
		d.OutputTokBefore += a.Before.MedianOutputTok
		d.OutputTokAfter += a.After.MedianOutputTok
		d.DurationMsBefore += a.Before.MedianDurationMs
		d.DurationMsAfter += a.After.MedianDurationMs
		d.ToolCallsBefore += a.Before.MedianToolCalls
		d.ToolCallsAfter += a.After.MedianToolCalls
	}
	return d
}

// Flaky returns the verdicts that are NOT cleanly stable on an identical
// Environment (a Before-vs-Before calibration): a rule that flipped
// (Regression/NewPass) despite no real change, or one whose N samples disagreed
// in either env. This is the noise floor a real Before-vs-After comparison
// stands on.
func Flaky(vs []Verdict) []Verdict {
	var out []Verdict
	for _, v := range vs {
		if v.Status == Regression || v.Status == NewPass || v.Before.Disagreement || v.After.Disagreement {
			out = append(out, v)
		}
	}
	return out
}

func median(vs []float64) float64 {
	n := len(vs)
	if n == 0 {
		return 0
	}
	s := append([]float64(nil), vs...)
	sort.Float64s(s)
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[n/2-1] + s[n/2]) / 2
}

func medianInt(vs []int) int {
	n := len(vs)
	if n == 0 {
		return 0
	}
	s := append([]int(nil), vs...)
	sort.Ints(s)
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[n/2-1] + s[n/2]) / 2
}
