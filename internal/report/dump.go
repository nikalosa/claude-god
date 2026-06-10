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

// DumpAnswers writes the judged Before/After answer pair for each probe to dir,
// one Markdown file per probe (NN-<id>.md) plus an index.md, so a human can read
// the two answers the judge actually compared (sample 1) side by side. prefs is
// indexed by probe (nil for a rule-based probe). It is report-only: the caller
// treats a write error as a warning so a failed dump never discards a run.
func DumpAnswers(dir, beforeRef, afterRef string, probes []dsl.Probe, before, after [][]*parser.RunRecord, prefs []*runner.PreferenceResult) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	var idx strings.Builder
	fmt.Fprintln(&idx, "# claude-validator answer dump")
	fmt.Fprintln(&idx)
	fmt.Fprintf(&idx, "Before = `%s` · After = `%s`. Each file holds the sample-1 pair the judge compared.\n", beforeRef, afterRef)
	fmt.Fprintln(&idx)
	fmt.Fprintln(&idx, "| # | Probe | Kind | Verdict | File |")
	fmt.Fprintln(&idx, "|---|---|---|---|---|")

	for pi, probe := range probes {
		name := fmt.Sprintf("%02d-%s.md", pi+1, dumpSlug.ReplaceAllString(probe.ID, "-"))
		var pref *runner.PreferenceResult
		if pi < len(prefs) {
			pref = prefs[pi]
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(probeDoc(probe, sampleOne(before, pi), sampleOne(after, pi), pref, beforeRef, afterRef)), 0o644); err != nil {
			return err
		}
		fmt.Fprintf(&idx, "| %d | %s | %s | %s | [%s](%s) |\n", pi+1, probe.ID, probe.Kind, verdictLabel(pref), name, name)
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

func answerMeta(rec *parser.RunRecord) string {
	if rec == nil {
		return "_(no record)_"
	}
	return fmt.Sprintf("_model %s · %d turns · $%.4f · %d in / %d out tok_",
		rec.Model, rec.NumTurns, rec.TotalCost, rec.TotalInputTokens(), rec.TotalOutputTokens())
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
