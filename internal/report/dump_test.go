package report

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nikalosa/claude-god/internal/dsl"
	"github.com/nikalosa/claude-god/internal/judge"
	"github.com/nikalosa/claude-god/internal/parser"
	"github.com/nikalosa/claude-god/internal/runner"
)

func rec(text string) *parser.RunRecord {
	return &parser.RunRecord{FinalText: text, Model: "m", NumTurns: 2, TotalCost: 0.5,
		Timing:            parser.Timing{DurationMs: 90000},
		PeakContextTokens: 40000,
		ModelUsage:        map[string]parser.ModelUsage{"m": {InputTokens: 100, OutputTokens: 20}}}
}

func TestDumpAnswers_WritesJudgedPair(t *testing.T) {
	dir := t.TempDir()
	probes := []dsl.Probe{
		{ID: "plan_thing", Prompt: "Plan it.", Kind: dsl.Plan},
		{ID: "rule_thing", Prompt: "Answer it.", Kind: dsl.RuleBased},
	}
	before := [][]*parser.RunRecord{{rec("BEFORE-PLAN"), rec("ignored-sample-2")}, {rec("BEFORE-RULE")}}
	after := [][]*parser.RunRecord{{rec("AFTER-PLAN")}, {rec("AFTER-RULE")}}
	prefs := []*runner.PreferenceResult{
		{ProbeID: "plan_thing", Outcome: judge.AfterBetter, Concise: judge.Tie, Exhaustive: judge.AfterBetter, Direct: judge.Tie, Reasoning: "after is clearer"},
		nil,
	}

	if err := DumpAnswers(dir, "staging", "slim", probes, before, after, prefs); err != nil {
		t.Fatalf("DumpAnswers: %v", err)
	}

	planDoc := readFile(t, filepath.Join(dir, "01-plan_thing.md"))
	for _, want := range []string{"# plan_thing", "BEFORE-PLAN", "AFTER-PLAN", "After reads better", "after is clearer", "`staging`", "`slim`", "step-by-step plan", "## Comparison", "Context window", "40.0k", "Time", "1m30s"} {
		if !strings.Contains(planDoc, want) {
			t.Errorf("plan doc missing %q\n%s", want, planDoc)
		}
	}
	if strings.Contains(planDoc, "ignored-sample-2") {
		t.Errorf("dump must use only sample 1, found sample 2 text:\n%s", planDoc)
	}

	ruleDoc := readFile(t, filepath.Join(dir, "02-rule_thing.md"))
	if !strings.Contains(ruleDoc, "BEFORE-RULE") || !strings.Contains(ruleDoc, "AFTER-RULE") {
		t.Errorf("rule doc missing answers\n%s", ruleDoc)
	}
	if !strings.Contains(ruleDoc, "no preference") {
		t.Errorf("rule-based doc should note it has no preference\n%s", ruleDoc)
	}

	idx := readFile(t, filepath.Join(dir, "index.md"))
	for _, want := range []string{"plan_thing", "rule_thing", "01-plan_thing.md", "02-rule_thing.md", "After reads better", "Context window (B→A)", "40.0k → 40.0k"} {
		if !strings.Contains(idx, want) {
			t.Errorf("index missing %q\n%s", want, idx)
		}
	}
}

func TestDumpAnswers_MissingRecord(t *testing.T) {
	dir := t.TempDir()
	probes := []dsl.Probe{{ID: "p", Prompt: "q", Kind: dsl.OpenEnded}}
	before := [][]*parser.RunRecord{nil}
	after := [][]*parser.RunRecord{{rec("AFTER")}}

	if err := DumpAnswers(dir, "b", "a", probes, before, after, []*runner.PreferenceResult{nil}); err != nil {
		t.Fatalf("DumpAnswers: %v", err)
	}
	doc := readFile(t, filepath.Join(dir, "01-p.md"))
	if !strings.Contains(doc, "(no record)") || !strings.Contains(doc, "AFTER") {
		t.Errorf("want placeholder for missing before and the real after\n%s", doc)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}
