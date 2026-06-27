package report

import (
	"fmt"
	"sort"
	"strings"

	"github.com/nikalosa/claude-god/internal/aggregator"
	"github.com/nikalosa/claude-god/internal/judge"
	"github.com/nikalosa/claude-god/internal/runner"
)

func RenderMarkdown(verdicts []aggregator.Verdict, prefs []runner.PreferenceResult, aggs []aggregator.AggregatedOutcome, concurrency int) string {
	var regressions, newPasses int
	for _, v := range verdicts {
		switch v.Status {
		case aggregator.Regression:
			regressions++
		case aggregator.NewPass:
			newPasses++
		}
	}

	var b strings.Builder
	fmt.Fprintln(&b, "# claude-benchmark report")
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "%d rules · %d regression(s) · %d new pass(es) · %d open-ended\n",
		len(verdicts), regressions, newPasses, len(prefs))
	fmt.Fprintln(&b)

	renderDeltas(&b, "Efficiency (Numbers)", aggregator.ComputeDeltas(aggs), concurrency)
	renderPerProbe(&b, aggs, concurrency)
	renderRules(&b, verdicts)
	renderPreferences(&b, prefs)

	return b.String()
}

func renderRules(b *strings.Builder, verdicts []aggregator.Verdict) {
	fmt.Fprintln(b, "## Rules")
	fmt.Fprintln(b)
	if len(verdicts) == 0 {
		fmt.Fprintln(b, "_none_")
		fmt.Fprintln(b)
		return
	}

	sorted := append([]aggregator.Verdict(nil), verdicts...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].ProbeID != sorted[j].ProbeID {
			return sorted[i].ProbeID < sorted[j].ProbeID
		}
		return sorted[i].RuleID < sorted[j].RuleID
	})

	fmt.Fprintln(b, "| Probe | Rule | Severity | Before | After | Status |")
	fmt.Fprintln(b, "|---|---|---|---|---|---|")
	for _, v := range sorted {
		fmt.Fprintf(b, "| %s | `%s` | %s | %s | %s | %s |\n",
			v.ProbeID, v.RuleID, v.Severity, fmtSide(v.Before), fmtSide(v.After), statusLabel(v.Status))
	}
	fmt.Fprintln(b)
}

func statusLabel(s aggregator.Status) string {
	switch s {
	case aggregator.Regression:
		return "regression"
	case aggregator.NewPass:
		return "new pass"
	case aggregator.StableFail:
		return "held (fail)"
	default:
		return "held"
	}
}

func RenderCalibration(verdicts []aggregator.Verdict, aggs []aggregator.AggregatedOutcome, concurrency int) string {
	flaky := aggregator.Flaky(verdicts)
	sort.Slice(flaky, func(i, j int) bool {
		if flaky[i].ProbeID != flaky[j].ProbeID {
			return flaky[i].ProbeID < flaky[j].ProbeID
		}
		return flaky[i].RuleID < flaky[j].RuleID
	})

	var b strings.Builder
	fmt.Fprintln(&b, "# claude-benchmark calibration (Before vs Before)")
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "Noise floor: %d of %d rules flaky on an identical Environment.\n", len(flaky), len(verdicts))
	fmt.Fprintln(&b)

	fmt.Fprintln(&b, "## Flaky rules")
	fmt.Fprintln(&b)
	if len(flaky) == 0 {
		fmt.Fprintln(&b, "_none — clean noise floor_")
	} else {
		for _, v := range flaky {
			fmt.Fprintf(&b, "- **[%s]** `%s` (%s) — %s → %s [%s]\n",
				v.ProbeID, v.RuleID, v.Severity, fmtSide(v.Before), fmtSide(v.After), flakyReason(v))
		}
	}
	fmt.Fprintln(&b)

	renderDeltas(&b, "Numbers spread (medians, summed across probes)", aggregator.ComputeDeltas(aggs), concurrency)
	renderPerProbe(&b, aggs, concurrency)
	return b.String()
}

func RenderAssessment(aggs []aggregator.AggregatedOutcome, envDesc string, concurrency int) string {
	var passed, failed, comparative int
	for _, a := range aggs {
		if len(a.Before.Rules) == 0 {
			comparative++
			continue
		}
		for _, r := range a.Before.Rules {
			if r.Pass {
				passed++
			} else {
				failed++
			}
		}
	}

	var b strings.Builder
	fmt.Fprintln(&b, "# claude-benchmark assessment (single environment)")
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "Environment: %s\n", envDesc)
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "%d rule(s) · %d passed · %d failed · %d comparative probe(s) not graded\n",
		passed+failed, passed, failed, comparative)
	fmt.Fprintln(&b)

	renderScorecard(&b, aggs)
	renderSingleNumbers(&b, aggregator.ComputeDeltas(aggs), concurrency)
	renderSinglePerProbe(&b, aggs, concurrency)
	renderNotGraded(&b, aggs)
	return b.String()
}

func renderScorecard(b *strings.Builder, aggs []aggregator.AggregatedOutcome) {
	type row struct {
		probe string
		rule  aggregator.AggregatedRuleResult
	}
	var rows []row
	for _, a := range aggs {
		for _, r := range a.Before.Rules {
			rows = append(rows, row{a.ProbeID, r})
		}
	}
	fmt.Fprintln(b, "## Scorecard")
	fmt.Fprintln(b)
	if len(rows) == 0 {
		fmt.Fprintln(b, "_no rule-based probes — Numbers only_")
		fmt.Fprintln(b)
		return
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].probe != rows[j].probe {
			return rows[i].probe < rows[j].probe
		}
		return rows[i].rule.RuleID < rows[j].rule.RuleID
	})
	fmt.Fprintln(b, "| Probe | Rule | Severity | Result |")
	fmt.Fprintln(b, "|---|---|---|---|")
	for _, r := range rows {
		side := aggregator.VerdictSide{Pass: r.rule.Pass, PassCount: r.rule.PassCount, Total: r.rule.Total, Disagreement: r.rule.Disagreement}
		fmt.Fprintf(b, "| %s | `%s` | %s | %s |\n", r.probe, r.rule.RuleID, r.rule.Severity, fmtSide(side))
	}
	fmt.Fprintln(b)
}

func renderSingleNumbers(b *strings.Builder, d aggregator.Deltas, concurrency int) {
	durLabel := "Duration (ms)"
	if concurrency > 1 {
		durLabel += " ⚠ advisory"
	}
	fmt.Fprintln(b, "## Numbers")
	fmt.Fprintln(b)
	fmt.Fprintln(b, "| Metric | Value |")
	fmt.Fprintln(b, "|---|---:|")
	fmt.Fprintf(b, "| Total cost (USD) | %.6f |\n", d.CostBefore)
	fmt.Fprintf(b, "| Input tokens (incl. cache) | %d |\n", d.InputTokBefore)
	fmt.Fprintf(b, "| Output tokens | %d |\n", d.OutputTokBefore)
	fmt.Fprintf(b, "| %s | %d |\n", durLabel, d.DurationMsBefore)
	fmt.Fprintf(b, "| Tool calls | %d |\n", d.ToolCallsBefore)
	fmt.Fprintf(b, "| Context window — base (turn-1) | %d |\n", d.BaseCtxBefore)
	fmt.Fprintf(b, "| Context window — peak ⚠ noisy | %d |\n", d.PeakCtxBefore)
	fmt.Fprintln(b)
	if concurrency > 1 {
		fmt.Fprintf(b, "> ⚠ Duration measured under --concurrency %d; advisory, not comparable. Rerun with --concurrency 1 for authoritative timing. Cost and tokens are exact regardless.\n\n", concurrency)
	}
	fmt.Fprintln(b, "> Context window = mean per-probe resident tokens. **Base (turn-1)** is the config-only prompt (system + CLAUDE.md + rules + memory + probe) before any exploration — deterministic, the clean config signal. **Peak** is the exploration high-water mark and is run-to-run noisy.")
	fmt.Fprintln(b)
}

func renderSinglePerProbe(b *strings.Builder, aggs []aggregator.AggregatedOutcome, concurrency int) {
	if len(aggs) == 0 {
		return
	}
	durHdr := "Duration (ms)"
	if concurrency > 1 {
		durHdr += " ⚠"
	}
	sorted := append([]aggregator.AggregatedOutcome(nil), aggs...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ProbeID < sorted[j].ProbeID })

	fmt.Fprintln(b, "## Per-probe Numbers")
	fmt.Fprintln(b)
	fmt.Fprintln(b, "_Medians across samples._")
	fmt.Fprintln(b)
	fmt.Fprintf(b, "| Probe | %s | Cost (USD) | Input tok | Output tok | Tool calls | Base ctx |\n", durHdr)
	fmt.Fprintln(b, "|---|---:|---:|---:|---:|---:|---:|")
	for _, a := range sorted {
		fmt.Fprintf(b, "| %s | %d | %.4f | %d | %d | %d | %d |\n", a.ProbeID,
			a.Before.MedianDurationMs, a.Before.MedianCost, a.Before.MedianInputTok,
			a.Before.MedianOutputTok, a.Before.MedianToolCalls, a.Before.MedianBaseCtx)
	}
	fmt.Fprintln(b)
}

func renderNotGraded(b *strings.Builder, aggs []aggregator.AggregatedOutcome) {
	var ids []string
	for _, a := range aggs {
		if len(a.Before.Rules) == 0 {
			ids = append(ids, a.ProbeID)
		}
	}
	if len(ids) == 0 {
		return
	}
	sort.Strings(ids)
	fmt.Fprintln(b, "## Not graded (comparative — needs A/B)")
	fmt.Fprintln(b)
	fmt.Fprintln(b, "_open_ended/plan probes are judged head-to-head; one Environment has nothing to prefer against. Run the A/B benchmark to compare two configs. Their Numbers are above._")
	fmt.Fprintln(b)
	for _, id := range ids {
		fmt.Fprintf(b, "- %s\n", id)
	}
	fmt.Fprintln(b)
}

func flakyReason(v aggregator.Verdict) string {
	if v.Status == aggregator.Regression || v.Status == aggregator.NewPass {
		return "flipped on identical input"
	}
	return "samples disagreed"
}

func renderDeltas(b *strings.Builder, title string, d aggregator.Deltas, concurrency int) {
	durLabel := "Duration (ms)"
	if concurrency > 1 {
		durLabel += " ⚠ advisory"
	}
	fmt.Fprintln(b, "## "+title)
	fmt.Fprintln(b)
	fmt.Fprintln(b, "| Metric | Before | After | Δ |")
	fmt.Fprintln(b, "|---|---:|---:|---:|")
	fmt.Fprintf(b, "| Total cost (USD) | %.6f | %.6f | %s |\n", d.CostBefore, d.CostAfter, deltaPct(d.CostBefore, d.CostAfter))
	fmt.Fprintf(b, "| Input tokens (incl. cache) | %d | %d | %s |\n", d.InputTokBefore, d.InputTokAfter, deltaIntPct(d.InputTokBefore, d.InputTokAfter))
	fmt.Fprintf(b, "| Output tokens | %d | %d | %s |\n", d.OutputTokBefore, d.OutputTokAfter, deltaIntPct(d.OutputTokBefore, d.OutputTokAfter))
	fmt.Fprintf(b, "| %s | %d | %d | %s |\n", durLabel, d.DurationMsBefore, d.DurationMsAfter, deltaIntPct(d.DurationMsBefore, d.DurationMsAfter))
	fmt.Fprintf(b, "| Tool calls | %d | %d | %s |\n", d.ToolCallsBefore, d.ToolCallsAfter, deltaIntPct(d.ToolCallsBefore, d.ToolCallsAfter))
	fmt.Fprintf(b, "| Context window — base (turn-1) | %d | %d | %s |\n", d.BaseCtxBefore, d.BaseCtxAfter, deltaIntPct(d.BaseCtxBefore, d.BaseCtxAfter))
	fmt.Fprintf(b, "| Context window — peak ⚠ noisy | %d | %d | %s |\n", d.PeakCtxBefore, d.PeakCtxAfter, deltaIntPct(d.PeakCtxBefore, d.PeakCtxAfter))
	fmt.Fprintln(b)
	if concurrency > 1 {
		fmt.Fprintf(b, "> ⚠ Duration measured under --concurrency %d; advisory, not comparable. Rerun with --concurrency 1 for authoritative timing. Cost and tokens are exact regardless.\n\n", concurrency)
	}
	fmt.Fprintln(b, "> Context window = mean per-probe resident tokens. **Base (turn-1)** is the config-only prompt (system + CLAUDE.md + rules + memory + probe) before any exploration — deterministic, the clean A/B signal. **Peak** is the exploration high-water mark and is run-to-run noisy.")
	fmt.Fprintln(b)
}

func renderPerProbe(b *strings.Builder, aggs []aggregator.AggregatedOutcome, concurrency int) {
	if len(aggs) == 0 {
		return
	}
	durHdr := "Duration (ms)"
	if concurrency > 1 {
		durHdr += " ⚠"
	}
	sorted := append([]aggregator.AggregatedOutcome(nil), aggs...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ProbeID < sorted[j].ProbeID })

	fmt.Fprintln(b, "## Per-probe Numbers")
	fmt.Fprintln(b)
	fmt.Fprintln(b, "_Before → After (Δ%). Medians across samples._")
	fmt.Fprintln(b)
	fmt.Fprintf(b, "| Probe | %s | Cost (USD) | Input tok | Output tok | Tool calls | Base ctx |\n", durHdr)
	fmt.Fprintln(b, "|---|---|---|---|---|---|---|")
	for _, a := range sorted {
		fmt.Fprintf(b, "| %s | %s | %s | %s | %s | %s | %s |\n", a.ProbeID,
			cellInt(a.Before.MedianDurationMs, a.After.MedianDurationMs),
			cellFloat(a.Before.MedianCost, a.After.MedianCost),
			cellInt(a.Before.MedianInputTok, a.After.MedianInputTok),
			cellInt(a.Before.MedianOutputTok, a.After.MedianOutputTok),
			cellInt(a.Before.MedianToolCalls, a.After.MedianToolCalls),
			cellInt(a.Before.MedianBaseCtx, a.After.MedianBaseCtx))
	}
	d := aggregator.ComputeDeltas(sorted)
	fmt.Fprintf(b, "| **TOTAL** (Base ctx: mean) | %s | %s | %s | %s | %s | %s |\n",
		cellInt(d.DurationMsBefore, d.DurationMsAfter),
		cellFloat(d.CostBefore, d.CostAfter),
		cellInt(d.InputTokBefore, d.InputTokAfter),
		cellInt(d.OutputTokBefore, d.OutputTokAfter),
		cellInt(d.ToolCallsBefore, d.ToolCallsAfter),
		cellInt(d.BaseCtxBefore, d.BaseCtxAfter))
	fmt.Fprintln(b)
}

func cellFloat(before, after float64) string {
	return fmt.Sprintf("%.4f → %.4f (%s)", before, after, pct(after-before, before))
}

func cellInt(before, after int) string {
	return fmt.Sprintf("%d → %d (%s)", before, after, pct(float64(after-before), float64(before)))
}

func pct(delta, before float64) string {
	if before == 0 {
		return "—"
	}
	return fmt.Sprintf("%+.1f%%", delta/before*100)
}

func renderPreferences(b *strings.Builder, prefs []runner.PreferenceResult) {
	if len(prefs) == 0 {
		return
	}
	sorted := append([]runner.PreferenceResult(nil), prefs...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ProbeID < sorted[j].ProbeID })

	fmt.Fprintln(b, "## What reads better (open-ended)")
	fmt.Fprintln(b)
	fmt.Fprintln(b, "_Report-only preference — no pass/fail, no gate._")
	fmt.Fprintln(b)
	for _, p := range sorted {
		fmt.Fprintf(b, "- **[%s]** %s (concise: %s, exhaustive: %s, direct: %s)\n",
			p.ProbeID, p.Outcome.Label(), prefShort(p.Concise), prefShort(p.Exhaustive), prefShort(p.Direct))
		if p.Reasoning != "" {
			fmt.Fprintf(b, "  - %s\n", p.Reasoning)
		}
	}
	fmt.Fprintln(b)
}

func prefShort(o judge.Outcome) string {
	switch o {
	case judge.BeforeBetter:
		return "before"
	case judge.AfterBetter:
		return "after"
	default:
		return "tie"
	}
}

func fmtSide(s aggregator.VerdictSide) string {
	verdict := "FAIL"
	if s.Pass {
		verdict = "PASS"
	}
	frac := fmt.Sprintf("%d/%d", s.PassCount, s.Total)
	if s.Disagreement {
		return fmt.Sprintf("%s (%s ⚠ disagreement)", verdict, frac)
	}
	return fmt.Sprintf("%s (%s)", verdict, frac)
}

func deltaPct(before, after float64) string {
	delta := after - before
	if before == 0 {
		return fmt.Sprintf("%+.6f", delta)
	}
	pct := delta / before * 100
	return fmt.Sprintf("%+.6f (%+.1f%%)", delta, pct)
}

func deltaIntPct(before, after int) string {
	delta := after - before
	if before == 0 {
		return fmt.Sprintf("%+d", delta)
	}
	pct := float64(delta) / float64(before) * 100
	return fmt.Sprintf("%+d (%+.1f%%)", delta, pct)
}
