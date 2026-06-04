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
	d := aggregator.Deltas{
		CostBefore: 0.20, CostAfter: 0.14,
		InputTokBefore: 2000, InputTokAfter: 1400,
		OutputTokBefore: 100, OutputTokAfter: 80,
		DurationMsBefore: 4000, DurationMsAfter: 3000,
		ToolCallsBefore: 24, ToolCallsAfter: 6,
	}
	md := RenderMarkdown(verdicts, prefs, d, 1)

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
	md := RenderMarkdown(verdicts, nil, aggregator.Deltas{}, 1)
	if !strings.Contains(md, "disagreement") {
		t.Errorf("disagreement marker missing: %s", md)
	}
}

func TestRenderMarkdown_EmptyBuckets(t *testing.T) {
	md := RenderMarkdown(nil, nil, aggregator.Deltas{}, 1)
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
	md := RenderMarkdown(nil, prefs, aggregator.Deltas{}, 1)

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
	md := RenderCalibration(verdicts, aggregator.Deltas{}, 1)

	if !strings.Contains(md, "# claude-validator calibration") {
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
	md := RenderCalibration(verdicts, aggregator.Deltas{}, 1)
	if !strings.Contains(md, "Noise floor: 0 of 1") {
		t.Errorf("expected clean floor:\n%s", md)
	}
	if !strings.Contains(md, "_none — clean noise floor_") {
		t.Errorf("expected clean-floor marker:\n%s", md)
	}
}

func TestRenderMarkdown_NoPreferenceSectionWhenEmpty(t *testing.T) {
	md := RenderMarkdown(nil, nil, aggregator.Deltas{}, 1)
	if strings.Contains(md, "What reads better") {
		t.Errorf("preference section should be omitted when there are no open-ended probes:\n%s", md)
	}
}

// TestRenderDeltas_DurationAdvisory checks that Duration is flagged advisory
// under concurrency and left clean at --concurrency 1 — the report must never
// silently present an inflated timing number as comparable.
func TestRenderDeltas_DurationAdvisory(t *testing.T) {
	d := aggregator.Deltas{DurationMsBefore: 4000, DurationMsAfter: 3000}

	warned := RenderMarkdown(nil, nil, d, 8)
	if !strings.Contains(warned, "advisory") || !strings.Contains(warned, "--concurrency 8") {
		t.Errorf("expected a Duration advisory under concurrency:\n%s", warned)
	}

	clean := RenderMarkdown(nil, nil, d, 1)
	if strings.Contains(clean, "advisory") {
		t.Errorf("Duration must not be flagged advisory at --concurrency 1:\n%s", clean)
	}
}
