package report

import (
	"strings"
	"testing"

	"github.com/nikalosa/claude-god/internal/aggregator"
	"github.com/nikalosa/claude-god/internal/dsl"
)

func side(pass bool, p, n int) aggregator.VerdictSide {
	return aggregator.VerdictSide{Pass: pass, PassCount: p, Total: n, Disagreement: p != 0 && p != n}
}

func TestRenderMarkdown_OrderingAndSections(t *testing.T) {
	verdicts := []aggregator.Verdict{
		{ProbeID: "p1", RuleID: "stable_pass", Severity: dsl.Critical, Before: side(true, 3, 3), After: side(true, 3, 3), Status: aggregator.Stable},
		{ProbeID: "p1", RuleID: "regressed", Severity: dsl.Critical, Before: side(true, 3, 3), After: side(false, 0, 3), Status: aggregator.Regression},
		{ProbeID: "p2", RuleID: "newly_passes", Severity: dsl.High, Before: side(false, 0, 3), After: side(true, 3, 3), Status: aggregator.NewPass},
		{ProbeID: "p2", RuleID: "still_failing", Severity: dsl.Medium, Before: side(false, 0, 3), After: side(false, 0, 3), Status: aggregator.StableFail},
	}
	d := aggregator.Deltas{
		CostBefore: 0.20, CostAfter: 0.14,
		InputTokBefore: 2000, InputTokAfter: 1400,
		OutputTokBefore: 100, OutputTokAfter: 80,
		DurationMsBefore: 4000, DurationMsAfter: 3000,
	}
	md := RenderMarkdown(verdicts, d)

	idxCrit := strings.Index(md, "## Critical regressions")
	idxNew := strings.Index(md, "## New passes")
	idxDeltas := strings.Index(md, "## Cost / token / time deltas")
	idxOther := strings.Index(md, "## Other verdicts")
	if idxCrit < 0 || idxNew < 0 || idxDeltas < 0 || idxOther < 0 {
		t.Fatalf("missing section\n%s", md)
	}
	if !(idxCrit < idxNew && idxNew < idxDeltas && idxDeltas < idxOther) {
		t.Errorf("section ordering wrong: crit=%d new=%d deltas=%d other=%d", idxCrit, idxNew, idxDeltas, idxOther)
	}
	if !strings.Contains(md, "`regressed`") {
		t.Error("regression rule not listed")
	}
	if !strings.Contains(md, "`newly_passes`") {
		t.Error("new pass rule not listed")
	}
	if !strings.Contains(md, "PASS (3/3)") {
		t.Errorf("pass-count not rendered: %s", md)
	}
	if !strings.Contains(md, "0.140000") {
		t.Error("after cost not rendered")
	}
}

func TestRenderMarkdown_Disagreement(t *testing.T) {
	verdicts := []aggregator.Verdict{
		{ProbeID: "p1", RuleID: "flaky", Severity: dsl.Medium, Before: side(true, 2, 3), After: side(true, 2, 3), Status: aggregator.Stable},
	}
	md := RenderMarkdown(verdicts, aggregator.Deltas{})
	if !strings.Contains(md, "disagreement") {
		t.Errorf("disagreement marker missing: %s", md)
	}
}

func TestRenderMarkdown_EmptyBuckets(t *testing.T) {
	md := RenderMarkdown(nil, aggregator.Deltas{})
	if !strings.Contains(md, "_none_") {
		t.Errorf("expected empty bucket markers\n%s", md)
	}
}
