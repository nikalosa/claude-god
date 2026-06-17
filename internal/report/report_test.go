package report

import (
	"strings"
	"testing"

	"github.com/nikalosa/claude-god/internal/aggregator"
	"github.com/nikalosa/claude-god/internal/dsl"
	"github.com/nikalosa/claude-god/internal/judge"
	"github.com/nikalosa/claude-god/internal/runner"
)

func side(pass bool, p, n int) aggregator.VerdictSide {
	return aggregator.VerdictSide{Pass: pass, PassCount: p, Total: n, Disagreement: p != 0 && p != n}
}

func TestRenderMarkdown_EfficiencyFirstFoldedMatrix(t *testing.T) {
	verdicts := []aggregator.Verdict{
		{ProbeID: "p1", RuleID: "stable_pass", Severity: dsl.Critical, Before: side(true, 3, 3), After: side(true, 3, 3), Status: aggregator.Stable},
		{ProbeID: "p1", RuleID: "regressed", Severity: dsl.Critical, Before: side(true, 3, 3), After: side(false, 0, 3), Status: aggregator.Regression},
		{ProbeID: "p2", RuleID: "newly_passes", Severity: dsl.High, Before: side(false, 0, 3), After: side(true, 3, 3), Status: aggregator.NewPass},
		{ProbeID: "p2", RuleID: "still_failing", Severity: dsl.Medium, Before: side(false, 0, 3), After: side(false, 0, 3), Status: aggregator.StableFail},
	}
	prefs := []runner.PreferenceResult{{ProbeID: "design", Outcome: judge.AfterBetter}}
	aggs := []aggregator.AggregatedOutcome{{
		ProbeID: "p1",
		Before:  aggregator.AggregatedEnv{MedianCost: 0.20, MedianInputTok: 2000, MedianOutputTok: 100, MedianDurationMs: 4000, MedianToolCalls: 24},
		After:   aggregator.AggregatedEnv{MedianCost: 0.14, MedianInputTok: 1400, MedianOutputTok: 80, MedianDurationMs: 3000, MedianToolCalls: 6},
	}}
	md := RenderMarkdown(verdicts, prefs, aggs, 1)

	// Efficiency (Numbers) leads, then the rule matrix, then the preference read.
	idxEff := strings.Index(md, "## Efficiency (Numbers)")
	idxRules := strings.Index(md, "## Rules")
	idxPrefs := strings.Index(md, "## What reads better (open-ended)")
	if idxEff < 0 || idxRules < 0 || idxPrefs < 0 {
		t.Fatalf("missing section\n%s", md)
	}
	if !(idxEff < idxRules && idxRules < idxPrefs) {
		t.Errorf("efficiency must lead, then rules, then preferences: eff=%d rules=%d prefs=%d", idxEff, idxRules, idxPrefs)
	}

	// The old gate-framed headline sections are gone.
	for _, gone := range []string{"## Critical regressions", "## New passes", "## Other verdicts"} {
		if strings.Contains(md, gone) {
			t.Errorf("old section %q must be removed:\n%s", gone, md)
		}
	}

	// One folded matrix carries every rule, with regression/new-pass demoted to a Status cell.
	for _, want := range []string{"| Status |", "`stable_pass`", "`regressed`", "`newly_passes`", "`still_failing`", "regression", "new pass"} {
		if !strings.Contains(md, want) {
			t.Errorf("folded matrix missing %q:\n%s", want, md)
		}
	}
	if !strings.Contains(md, "PASS (3/3)") {
		t.Errorf("pass-count not rendered:\n%s", md)
	}
	if !strings.Contains(md, "0.140000") {
		t.Error("after cost not rendered")
	}
	if !strings.Contains(md, "| Tool calls | 24 | 6 |") {
		t.Errorf("tool-calls delta row not rendered:\n%s", md)
	}
}

func TestRenderMarkdown_Disagreement(t *testing.T) {
	verdicts := []aggregator.Verdict{
		{ProbeID: "p1", RuleID: "flaky", Severity: dsl.Medium, Before: side(true, 2, 3), After: side(true, 2, 3), Status: aggregator.Stable},
	}
	md := RenderMarkdown(verdicts, nil, nil, 1)
	if !strings.Contains(md, "disagreement") {
		t.Errorf("disagreement marker missing: %s", md)
	}
}

func TestRenderMarkdown_EmptyBuckets(t *testing.T) {
	md := RenderMarkdown(nil, nil, nil, 1)
	if !strings.Contains(md, "_none_") {
		t.Errorf("expected empty bucket markers\n%s", md)
	}
}

func TestRenderMarkdown_PreferenceSection(t *testing.T) {
	prefs := []runner.PreferenceResult{{
		ProbeID: "design_tradeoffs", Outcome: judge.AfterBetter,
		Concise: judge.AfterBetter, Exhaustive: judge.Tie, Direct: judge.AfterBetter,
		Reasoning: "the after answer is tighter",
	}}
	md := RenderMarkdown(nil, prefs, nil, 1)

	const heading = "## What reads better (open-ended)"
	start := strings.Index(md, heading)
	if start < 0 {
		t.Fatalf("missing preference section:\n%s", md)
	}
	// Isolate the section (until the next "## " heading) and assert it carries
	// no PASS/FAIL or severity — it is strictly report-only.
	section := md[start+len(heading):]
	if end := strings.Index(section, "\n## "); end >= 0 {
		section = section[:end]
	}
	if !strings.Contains(section, "design_tradeoffs") || !strings.Contains(section, "After reads better") {
		t.Errorf("preference outcome not rendered:\n%s", section)
	}
	if !strings.Contains(section, "the after answer is tighter") {
		t.Errorf("preference reasoning not rendered:\n%s", section)
	}
	for _, banned := range []string{"PASS", "FAIL", "critical", "high", "medium"} {
		if strings.Contains(section, banned) {
			t.Errorf("preference section must not contain %q (report-only):\n%s", banned, section)
		}
	}
}

func TestRenderCalibration(t *testing.T) {
	verdicts := []aggregator.Verdict{
		{ProbeID: "p1", RuleID: "stable", Severity: dsl.High, Before: side(true, 3, 3), After: side(true, 3, 3), Status: aggregator.Stable},
		{ProbeID: "p1", RuleID: "flipped", Severity: dsl.Critical, Before: side(true, 3, 3), After: side(false, 0, 3), Status: aggregator.Regression},
	}
	md := RenderCalibration(verdicts, nil, 1)

	if !strings.Contains(md, "# claude-benchmark calibration") {
		t.Errorf("missing calibration title:\n%s", md)
	}
	if !strings.Contains(md, "Noise floor: 1 of 2") {
		t.Errorf("missing/incorrect noise floor:\n%s", md)
	}
	if !strings.Contains(md, "`flipped`") {
		t.Errorf("flaky rule not listed:\n%s", md)
	}
	if strings.Contains(md, "`stable`") {
		t.Errorf("a clean rule must not appear in the flaky list:\n%s", md)
	}
	if !strings.Contains(md, "## Numbers spread") {
		t.Errorf("missing Numbers spread section:\n%s", md)
	}
}

func TestRenderCalibration_CleanFloor(t *testing.T) {
	verdicts := []aggregator.Verdict{
		{ProbeID: "p1", RuleID: "stable", Severity: dsl.High, Before: side(true, 3, 3), After: side(true, 3, 3), Status: aggregator.Stable},
	}
	md := RenderCalibration(verdicts, nil, 1)
	if !strings.Contains(md, "Noise floor: 0 of 1") {
		t.Errorf("expected clean floor:\n%s", md)
	}
	if !strings.Contains(md, "_none — clean noise floor_") {
		t.Errorf("expected clean-floor marker:\n%s", md)
	}
}

func TestRenderMarkdown_NoPreferenceSectionWhenEmpty(t *testing.T) {
	md := RenderMarkdown(nil, nil, nil, 1)
	if strings.Contains(md, "What reads better") {
		t.Errorf("preference section should be omitted when there are no open-ended probes:\n%s", md)
	}
}

// TestRenderAssessment checks the single-env scorecard: per-rule PASS/FAIL with
// no A/B framing, single-env Numbers (a Value column, no Δ), and comparative
// probes listed as not graded.
func TestRenderAssessment(t *testing.T) {
	aggs := []aggregator.AggregatedOutcome{
		{ProbeID: "rules_probe", Before: aggregator.AggregatedEnv{
			MedianCost: 0.12, MedianInputTok: 1500, MedianOutputTok: 90, MedianDurationMs: 3000,
			MedianToolCalls: 5, MedianBaseCtx: 12000, MedianPeakCtx: 40000,
			Rules: []aggregator.AggregatedRuleResult{
				{RuleID: "kept", Severity: dsl.Critical, Pass: true, PassCount: 3, Total: 3},
				{RuleID: "dropped", Severity: dsl.High, Pass: false, PassCount: 0, Total: 3},
			},
		}},
		{ProbeID: "design_probe", Before: aggregator.AggregatedEnv{
			MedianCost: 0.50, MedianInputTok: 3000, MedianOutputTok: 200, MedianDurationMs: 9000,
			MedianToolCalls: 18, MedianBaseCtx: 12000, MedianPeakCtx: 80000,
		}},
	}
	md := RenderAssessment(aggs, "working tree (abc1234)", 1)

	if !strings.Contains(md, "# claude-benchmark assessment (single environment)") {
		t.Fatalf("missing assessment title:\n%s", md)
	}
	if !strings.Contains(md, "working tree (abc1234)") {
		t.Errorf("env description not rendered:\n%s", md)
	}
	if !strings.Contains(md, "2 rule(s) · 1 passed · 1 failed · 1 comparative probe(s) not graded") {
		t.Errorf("summary counts wrong:\n%s", md)
	}
	for _, want := range []string{"## Scorecard", "`kept`", "`dropped`", "PASS (3/3)", "FAIL (0/3)", "| Result |"} {
		if !strings.Contains(md, want) {
			t.Errorf("scorecard missing %q:\n%s", want, md)
		}
	}
	if !strings.Contains(md, "## Numbers") || !strings.Contains(md, "| Metric | Value |") {
		t.Errorf("single-env Numbers section (no Δ) missing:\n%s", md)
	}
	if !strings.Contains(md, "## Not graded (comparative — needs A/B)") || !strings.Contains(md, "design_probe") {
		t.Errorf("comparative not-graded list missing:\n%s", md)
	}

	// The scorecard section must carry no A/B framing: no Before/After/Status/Δ.
	start := strings.Index(md, "## Scorecard")
	section := md[start:]
	if end := strings.Index(section[len("## Scorecard"):], "\n## "); end >= 0 {
		section = section[:len("## Scorecard")+end]
	}
	for _, banned := range []string{"Before", "After", "Status", "Δ", "regression", "new pass"} {
		if strings.Contains(section, banned) {
			t.Errorf("scorecard must not contain A/B framing %q:\n%s", banned, section)
		}
	}
}

// TestRenderAssessment_NumbersOnly: a corpus of only comparative probes has no
// scorecard rows but still reports Numbers and the not-graded list.
func TestRenderAssessment_NumbersOnly(t *testing.T) {
	aggs := []aggregator.AggregatedOutcome{
		{ProbeID: "design", Before: aggregator.AggregatedEnv{MedianCost: 0.3, MedianBaseCtx: 9000}},
	}
	md := RenderAssessment(aggs, "HEAD (deadbeef)", 1)
	if !strings.Contains(md, "_no rule-based probes — Numbers only_") {
		t.Errorf("expected empty-scorecard marker:\n%s", md)
	}
	if !strings.Contains(md, "0 rule(s) · 0 passed · 0 failed · 1 comparative probe(s) not graded") {
		t.Errorf("summary wrong for Numbers-only:\n%s", md)
	}
}

// TestRenderDeltas_DurationAdvisory checks that Duration is flagged advisory
// under concurrency and left clean at --concurrency 1 — the report must never
// silently present an inflated timing number as comparable.
func TestRenderDeltas_DurationAdvisory(t *testing.T) {
	aggs := []aggregator.AggregatedOutcome{{
		ProbeID: "p1",
		Before:  aggregator.AggregatedEnv{MedianDurationMs: 4000},
		After:   aggregator.AggregatedEnv{MedianDurationMs: 3000},
	}}

	warned := RenderMarkdown(nil, nil, aggs, 8)
	if !strings.Contains(warned, "advisory") || !strings.Contains(warned, "--concurrency 8") {
		t.Errorf("expected a Duration advisory under concurrency:\n%s", warned)
	}

	clean := RenderMarkdown(nil, nil, aggs, 1)
	if strings.Contains(clean, "advisory") {
		t.Errorf("Duration must not be flagged advisory at --concurrency 1:\n%s", clean)
	}
}

// TestRenderPerProbe checks the per-probe Numbers table: one row per probe with
// before → after (Δ%) and a summed TOTAL row, so the dev sees which probe drives
// cost and time.
func TestRenderPerProbe(t *testing.T) {
	aggs := []aggregator.AggregatedOutcome{
		{ProbeID: "cheap_rule", Before: aggregator.AggregatedEnv{MedianCost: 0.10, MedianDurationMs: 20000, MedianToolCalls: 0}, After: aggregator.AggregatedEnv{MedianCost: 0.08, MedianDurationMs: 18000, MedianToolCalls: 0}},
		{ProbeID: "pricey_design", Before: aggregator.AggregatedEnv{MedianCost: 2.00, MedianDurationMs: 150000, MedianToolCalls: 20}, After: aggregator.AggregatedEnv{MedianCost: 1.40, MedianDurationMs: 170000, MedianToolCalls: 35}},
	}
	md := RenderMarkdown(nil, nil, aggs, 1)

	if !strings.Contains(md, "## Per-probe Numbers") {
		t.Fatalf("missing per-probe section:\n%s", md)
	}
	for _, want := range []string{"cheap_rule", "pricey_design", "**TOTAL**"} {
		if !strings.Contains(md, want) {
			t.Errorf("per-probe table missing %q:\n%s", want, md)
		}
	}
	// per-probe time direction must be visible: the design probe got slower After.
	if !strings.Contains(md, "150000 → 170000 (+13.3%)") {
		t.Errorf("per-probe duration cell not rendered as before → after (Δ%%):\n%s", md)
	}
	// TOTAL sums the medians: cost 2.10 → 1.48.
	if !strings.Contains(md, "2.1000 → 1.4800") {
		t.Errorf("TOTAL row not summed:\n%s", md)
	}
}
