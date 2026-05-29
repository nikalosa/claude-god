package report

import (
	"fmt"
	"sort"
	"strings"

	"github.com/nikalosa/claude-god/internal/aggregator"
)

func RenderMarkdown(verdicts []aggregator.Verdict, d aggregator.Deltas) string {
	regressions, newPasses, others := bucket(verdicts)

	var b strings.Builder
	fmt.Fprintln(&b, "# claude-validator report")
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "%d verdicts: %d critical regressions, %d new passes, %d other\n",
		len(verdicts), countCritical(regressions), len(newPasses), len(others))
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

	fmt.Fprintln(&b, "## Cost / token / time deltas (medians, summed across probes)")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "| Metric | Before | After | Δ |")
	fmt.Fprintln(&b, "|---|---:|---:|---:|")
	fmt.Fprintf(&b, "| Total cost (USD) | %.6f | %.6f | %s |\n", d.CostBefore, d.CostAfter, deltaPct(d.CostBefore, d.CostAfter))
	fmt.Fprintf(&b, "| Input tokens | %d | %d | %s |\n", d.InputTokBefore, d.InputTokAfter, deltaIntPct(d.InputTokBefore, d.InputTokAfter))
	fmt.Fprintf(&b, "| Output tokens | %d | %d | %s |\n", d.OutputTokBefore, d.OutputTokAfter, deltaIntPct(d.OutputTokBefore, d.OutputTokAfter))
	fmt.Fprintf(&b, "| Duration (ms) | %d | %d | %s |\n", d.DurationMsBefore, d.DurationMsAfter, deltaIntPct(d.DurationMsBefore, d.DurationMsAfter))
	fmt.Fprintln(&b)

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
