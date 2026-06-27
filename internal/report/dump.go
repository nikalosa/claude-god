package report

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/nikalosa/claude-god/internal/dsl"
	"github.com/nikalosa/claude-god/internal/parser"
	"github.com/nikalosa/claude-god/internal/runner"
)

var dumpSlug = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func DumpAnswers(dir, beforeRef, afterRef string, probes []dsl.Probe, before, after [][]*parser.RunRecord, prefs []*runner.PreferenceResult) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	var idx strings.Builder
	fmt.Fprintln(&idx, "# claude-benchmark answer dump")
	fmt.Fprintln(&idx)
	fmt.Fprintf(&idx, "Before = `%s` · After = `%s`. Sample 1 per probe.\n", beforeRef, afterRef)
	fmt.Fprintln(&idx)
	fmt.Fprintln(&idx, "Context window = peak resident tokens (high-water mark across turns, ≈ the Claude Code status line). Time = wall-clock per run. Δ is After vs Before.")
	fmt.Fprintln(&idx)
	fmt.Fprintln(&idx, "| # | Probe | Context window (B→A) | Time (B→A) | Verdict | File |")
	fmt.Fprintln(&idx, "|---|---|---|---|---|---|")

	for pi, probe := range probes {
		name := fmt.Sprintf("%02d-%s.md", pi+1, dumpSlug.ReplaceAllString(probe.ID, "-"))
		var pref *runner.PreferenceResult
		if pi < len(prefs) {
			pref = prefs[pi]
		}
		b := sampleOne(before, pi)
		a := sampleOne(after, pi)
		if err := os.WriteFile(filepath.Join(dir, name), []byte(probeDoc(probe, b, a, pref, beforeRef, afterRef)), 0o644); err != nil {
			return err
		}
		fmt.Fprintf(&idx, "| %d | %s | %s | %s | %s | [%s](%s) |\n",
			pi+1, probe.ID, ctxCell(b, a), timeCell(b, a), verdictLabel(pref), name, name)
	}
	fmt.Fprintln(&idx)

	return os.WriteFile(filepath.Join(dir, "index.md"), []byte(idx.String()), 0o644)
}

func sampleOne(recs [][]*parser.RunRecord, pi int) *parser.RunRecord {
	if pi < len(recs) && len(recs[pi]) > 0 {
		return recs[pi][0]
	}
	return nil
}

func probeDoc(probe dsl.Probe, before, after *parser.RunRecord, pref *runner.PreferenceResult, beforeRef, afterRef string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", probe.ID)
	fmt.Fprintf(&b, "**Kind:** %s\n\n", probe.Kind)
	if probe.Kind == dsl.Plan {
		fmt.Fprintln(&b, "_Run was asked for a step-by-step plan, not execution._")
		fmt.Fprintln(&b)
	}
	fmt.Fprintf(&b, "**Prompt:** %s\n\n", probe.Prompt)

	fmt.Fprintf(&b, "## Comparison\n\n%s\n", comparison(before, after, beforeRef, afterRef))

	if pref != nil {
		fmt.Fprintf(&b, "**Verdict:** %s (concise: %s, exhaustive: %s, direct: %s)\n\n",
			pref.Outcome.Label(), prefShort(pref.Concise), prefShort(pref.Exhaustive), prefShort(pref.Direct))
		if pref.Reasoning != "" {
			fmt.Fprintf(&b, "**Reasoning:** %s\n\n", pref.Reasoning)
		}
	} else {
		fmt.Fprintln(&b, "_Rule-based probe — graded by rules (see report); no preference._")
		fmt.Fprintln(&b)
	}

	fmt.Fprintf(&b, "---\n\n## Before — `%s`\n\n%s\n\n%s\n\n", beforeRef, answerMeta(before), answerText(before))
	fmt.Fprintf(&b, "---\n\n## After — `%s`\n\n%s\n\n%s\n", afterRef, answerMeta(after), answerText(after))
	return b.String()
}

func comparison(before, after *parser.RunRecord, beforeRef, afterRef string) string {
	if before == nil || after == nil {
		return "_Comparison unavailable — a run record is missing._\n"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "| Metric | Before `%s` | After `%s` | Δ |\n", beforeRef, afterRef)
	fmt.Fprintln(&b, "|---|---|---|---|")
	fmt.Fprintf(&b, "| Context window | %s | %s | %s |\n",
		tokK(before.ContextWindowTokens()), tokK(after.ContextWindowTokens()),
		pctDelta(before.ContextWindowTokens(), after.ContextWindowTokens()))
	fmt.Fprintf(&b, "| Time | %s | %s | %s |\n",
		dur(before.Timing.DurationMs), dur(after.Timing.DurationMs),
		pctDelta(before.Timing.DurationMs, after.Timing.DurationMs))
	fmt.Fprintf(&b, "| Turns | %d | %d | %s |\n", before.NumTurns, after.NumTurns, intDelta(before.NumTurns, after.NumTurns))
	fmt.Fprintf(&b, "| Cost | $%.4f | $%.4f | %s |\n", before.TotalCost, after.TotalCost,
		pctDelta(int(before.TotalCost*1e6), int(after.TotalCost*1e6)))
	return b.String()
}

func ctxCell(before, after *parser.RunRecord) string {
	if before == nil || after == nil {
		return "—"
	}
	return fmt.Sprintf("%s → %s (%s)", tokK(before.ContextWindowTokens()), tokK(after.ContextWindowTokens()),
		pctDelta(before.ContextWindowTokens(), after.ContextWindowTokens()))
}

func timeCell(before, after *parser.RunRecord) string {
	if before == nil || after == nil {
		return "—"
	}
	return fmt.Sprintf("%s → %s (%s)", dur(before.Timing.DurationMs), dur(after.Timing.DurationMs),
		pctDelta(before.Timing.DurationMs, after.Timing.DurationMs))
}

func answerMeta(rec *parser.RunRecord) string {
	if rec == nil {
		return "_(no record)_"
	}
	return fmt.Sprintf("_model %s · %d turns · %s · context %s tok · $%.4f_",
		rec.Model, rec.NumTurns, dur(rec.Timing.DurationMs), tokK(rec.ContextWindowTokens()), rec.TotalCost)
}

func answerText(rec *parser.RunRecord) string {
	if rec == nil || strings.TrimSpace(rec.FinalText) == "" {
		return "_(empty answer)_"
	}
	return rec.FinalText
}

func verdictLabel(pref *runner.PreferenceResult) string {
	if pref == nil {
		return "—"
	}
	return pref.Outcome.Label()
}

func tokK(n int) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 1_000:
		return fmt.Sprintf("%.1fk", float64(n)/1e3)
	default:
		return fmt.Sprintf("%d", n)
	}
}

func dur(ms int) string {
	s := ms / 1000
	if s >= 60 {
		return fmt.Sprintf("%dm%02ds", s/60, s%60)
	}
	return fmt.Sprintf("%ds", s)
}

func pctDelta(before, after int) string {
	if before == 0 {
		return "—"
	}
	return fmt.Sprintf("%+.1f%%", (float64(after)-float64(before))/float64(before)*100)
}

func intDelta(before, after int) string {
	return fmt.Sprintf("%+d", after-before)
}
