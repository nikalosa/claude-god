package report

import (
	"fmt"
	"sort"
	"strings"

	"github.com/nikalosa/claude-god/internal/aggregator"
	"github.com/nikalosa/claude-god/internal/judge"
	"github.com/nikalosa/claude-god/internal/runner"
)

func RenderMarkdown(verdicts []aggregator.Verdict, prefs []runner.PreferenceResult, d aggregator.Deltas) string {
	regressions, newPasses, others := bucket(verdicts)

	var b strings.Builder
	fmt.Fprintln(&b, "# claude-validator report")
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "%d verdicts: %d critical regressions, %d new passes, %d other; %d open-ended\n",
		len(verdicts), countCritical(regressions), len(newPasses), len(others), len(prefs))
	fmt.Fprintln(&b)

	fmt.Fprintln(&b, "## Critical regressions")
	fmt.Fprintln(&b)
	if len(regressions) == 0 {
		fmt.Fprintln(&b, "_none_")
	} else {
		for _, v := range regressions {
			fmt.Fprintf(&b, "- **[%s]** `%s` (%s) — %s → %s\n",
				v.ProbeID, v.RuleID, v.Severity, fmtSide(v.Before), fmtSide(v.After))
		}
	}
	fmt.Fprintln(&b)

	fmt.Fprintln(&b, "## New passes")
	fmt.Fprintln(&b)
	if len(newPasses) == 0 {
		fmt.Fprintln(&b, "_none_")
	} else {
		for _, v := range newPasses {
			fmt.Fprintf(&b, "- **[%s]** `%s` (%s) — %s → %s\n",
				v.ProbeID, v.RuleID, v.Severity, fmtSide(v.Before), fmtSide(v.After))
		}
	}
	fmt.Fprintln(&b)

	renderPreferences(&b, prefs)

	renderDeltas(&b, "Cost / token / time deltas (medians, summed across probes)", d)

	if len(others) > 0 {
		fmt.Fprintln(&b, "## Other verdicts")
		fmt.Fprintln(&b)
		fmt.Fprintln(&b, "| Probe | Rule | Severity | Before | After | Status |")
		fmt.Fprintln(&b, "|---|---|---|---|---|---|")
		for _, v := range others {
			fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s |\n",
				v.ProbeID, v.RuleID, v.Severity, fmtSide(v.Before), fmtSide(v.After), v.Status)
		}
	}

	return b.String()
}

// RenderCalibration renders a Before-vs-Before noise-floor report: the rules
// that came out flaky on an identical Environment (the false-positive rate a
// real comparison stands on), plus the Numbers spread. Never gates.
func RenderCalibration(verdicts []aggregator.Verdict, d aggregator.Deltas) string {
	flaky := aggregator.Flaky(verdicts)
	sort.Slice(flaky, func(i, j int) bool {
		if flaky[i].ProbeID != flaky[j].ProbeID {
			return flaky[i].ProbeID < flaky[j].ProbeID
		}
		return flaky[i].RuleID < flaky[j].RuleID
	})

	var b strings.Builder
	fmt.Fprintln(&b, "# claude-validator calibration (Before vs Before)")
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

	renderDeltas(&b, "Numbers spread (medians, summed across probes)", d)
	return b.String()
}

func flakyReason(v aggregator.Verdict) string {
	if v.Status == aggregator.Regression || v.Status == aggregator.NewPass {
		return "flipped on identical input"
	}
	return "samples disagreed"
}

func renderDeltas(b *strings.Builder, title string, d aggregator.Deltas) {
	fmt.Fprintln(b, "## "+title)
	fmt.Fprintln(b)
	fmt.Fprintln(b, "| Metric | Before | After | Δ |")
	fmt.Fprintln(b, "|---|---:|---:|---:|")
	fmt.Fprintf(b, "| Total cost (USD) | %.6f | %.6f | %s |\n", d.CostBefore, d.CostAfter, deltaPct(d.CostBefore, d.CostAfter))
	fmt.Fprintf(b, "| Input tokens (incl. cache) | %d | %d | %s |\n", d.InputTokBefore, d.InputTokAfter, deltaIntPct(d.InputTokBefore, d.InputTokAfter))
	fmt.Fprintf(b, "| Output tokens | %d | %d | %s |\n", d.OutputTokBefore, d.OutputTokAfter, deltaIntPct(d.OutputTokBefore, d.OutputTokAfter))
	fmt.Fprintf(b, "| Duration (ms) | %d | %d | %s |\n", d.DurationMsBefore, d.DurationMsAfter, deltaIntPct(d.DurationMsBefore, d.DurationMsAfter))
	fmt.Fprintf(b, "| Tool calls | %d | %d | %s |\n", d.ToolCallsBefore, d.ToolCallsAfter, deltaIntPct(d.ToolCallsBefore, d.ToolCallsAfter))
	fmt.Fprintln(b)
}

// renderPreferences renders the open-ended "what reads better" section. It is
// strictly report-only: no PASS/FAIL, no severity, and these results never
// become Verdicts, so they cannot affect the exit code.
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

func bucket(vs []aggregator.Verdict) (regressions, newPasses, others []aggregator.Verdict) {
	for _, v := range vs {
		switch v.Status {
		case aggregator.Regression:
			regressions = append(regressions, v)
		case aggregator.NewPass:
			newPasses = append(newPasses, v)
		default:
			others = append(others, v)
		}
	}
	sortByID := func(s []aggregator.Verdict) {
		sort.Slice(s, func(i, j int) bool {
			if s[i].ProbeID != s[j].ProbeID {
				return s[i].ProbeID < s[j].ProbeID
			}
			return s[i].RuleID < s[j].RuleID
		})
	}
	sortByID(regressions)
	sortByID(newPasses)
	sortByID(others)
	return
}

func countCritical(vs []aggregator.Verdict) int {
	n := 0
	for _, v := range vs {
		if v.Severity == "critical" {
			n++
		}
	}
	return n
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
