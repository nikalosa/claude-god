package dsl

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/nikalosa/claude-god/internal/judge"
	"github.com/nikalosa/claude-god/internal/parser"
)

var ctx = context.Background()

func TestGrade_TextMatches(t *testing.T) {
	rec := &parser.RunRecord{FinalText: "Amounts are stored as strings to avoid float drift."}
	rules := []Rule{
		{ID: "money_as_string", Severity: Critical, Checks: []Check{
			&TextMatches{Pattern: regexp.MustCompile("(?i)string")},
		}},
		{ID: "mentions_drift", Severity: High, Checks: []Check{
			&TextMatches{Pattern: regexp.MustCompile("(?i)drift")},
		}},
		{ID: "mentions_postgres", Severity: Medium, Checks: []Check{
			&TextMatches{Pattern: regexp.MustCompile("(?i)postgres")},
		}},
	}
	got, err := Grade(ctx, "q", rec, rules, nil)
	if err != nil {
		t.Fatalf("Grade: %v", err)
	}
	want := []RuleResult{
		{RuleID: "money_as_string", Severity: Critical, Pass: true},
		{RuleID: "mentions_drift", Severity: High, Pass: true},
		{RuleID: "mentions_postgres", Severity: Medium, Pass: false},
	}
	if len(got) != len(want) {
		t.Fatalf("len mismatch: got %d want %d", len(got), len(want))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("rule %d: got %+v want %+v", i, got[i], want[i])
		}
	}
}

func TestGrade_MultiCheckAllMustPass(t *testing.T) {
	rec := &parser.RunRecord{FinalText: "Amounts are stored as strings."}
	rules := []Rule{
		{ID: "compound", Severity: Critical, Checks: []Check{
			&TextMatches{Pattern: regexp.MustCompile("(?i)string")},
			&TextMatches{Pattern: regexp.MustCompile("(?i)drift")}, // not in text
		}},
	}
	got, err := Grade(ctx, "q", rec, rules, nil)
	if err != nil {
		t.Fatalf("Grade: %v", err)
	}
	if got[0].Pass {
		t.Errorf("expected FAIL when one of multiple checks fails")
	}
}

func TestJudgeRubric_Threshold(t *testing.T) {
	rec := &parser.RunRecord{FinalText: "some answer"}
	c := &JudgeRubric{Facts: []string{"a", "b"}, PassScore: 50}
	cases := []struct {
		score int
		want  bool
	}{
		{100, true}, {50, true}, {49, false}, {0, false},
	}
	for _, tc := range cases {
		j := judge.StubJudge{ScoreValue: tc.score}
		ok, err := c.Eval(ctx, EvalInput{Prompt: "q", Record: rec, Judge: j})
		if err != nil {
			t.Fatalf("Eval(score=%d): %v", tc.score, err)
		}
		if ok != tc.want {
			t.Errorf("score %d (pass=%d): got %v want %v", tc.score, c.PassScore, ok, tc.want)
		}
	}
}

func TestJudgeRubric_NilJudge(t *testing.T) {
	c := &JudgeRubric{Facts: []string{"a"}, PassScore: 50}
	if _, err := c.Eval(ctx, EvalInput{Record: &parser.RunRecord{}, Judge: nil}); err == nil {
		t.Error("expected error when a judge_rubric check is reached with a nil judge")
	}
}

func TestGrade_JudgeRubric(t *testing.T) {
	rec := &parser.RunRecord{FinalText: "names the ledger and the grpc call"}
	rules := []Rule{{ID: "trace", Severity: High, Checks: []Check{
		&JudgeRubric{Facts: []string{"ledger", "grpc"}, PassScore: 60},
	}}}
	got, err := Grade(ctx, "trace it", rec, rules, judge.StubJudge{ScoreValue: 80})
	if err != nil {
		t.Fatalf("Grade: %v", err)
	}
	if len(got) != 1 || !got[0].Pass {
		t.Errorf("expected PASS, got %+v", got)
	}
}

func TestGrade_PropagatesJudgeError(t *testing.T) {
	rec := &parser.RunRecord{FinalText: "x"}
	rules := []Rule{{ID: "r", Severity: Critical, Checks: []Check{
		&JudgeRubric{Facts: []string{"f"}, PassScore: 50},
	}}}
	_, err := Grade(ctx, "q", rec, rules, judge.StubJudge{ScoreErr: errors.New("boom")})
	if err == nil {
		t.Error("expected Grade to propagate a judge error")
	}
}

// TestTextMatches_IgnoresJudge pins the determinism boundary: a regex check
// must never touch the judge, so its result is identical with a nil judge and
// with a judge that panics if called (ADR-0002: judge noise must not reach the
// deterministic pattern path).
func TestTextMatches_IgnoresJudge(t *testing.T) {
	rec := &parser.RunRecord{FinalText: "stored as strings"}
	c := &TextMatches{Pattern: regexp.MustCompile("(?i)string")}
	panicJudge := judge.StubJudge{ScoreFunc: func(context.Context, string, string, []string) (int, error) {
		panic("regex check must not call the judge")
	}}

	okNil, errNil := c.Eval(ctx, EvalInput{Record: rec, Judge: nil})
	okPanic, errPanic := c.Eval(ctx, EvalInput{Record: rec, Judge: panicJudge})
	if errNil != nil || errPanic != nil {
		t.Fatalf("unexpected errors: nil=%v panic=%v", errNil, errPanic)
	}
	if !okNil || !okPanic {
		t.Errorf("regex should match regardless of judge: nil=%v panic=%v", okNil, okPanic)
	}
}

// TestGrade_RegexFailShortCircuitsBeforeJudge confirms a failing regex check
// ordered before a judge check FAILs the rule without invoking the judge.
func TestGrade_RegexFailShortCircuitsBeforeJudge(t *testing.T) {
	rec := &parser.RunRecord{FinalText: "no keyword here"}
	rules := []Rule{{ID: "r", Severity: Critical, Checks: []Check{
		&TextMatches{Pattern: regexp.MustCompile("WILL_NOT_MATCH")},
		&JudgeRubric{Facts: []string{"f"}, PassScore: 50},
	}}}
	panicJudge := judge.StubJudge{ScoreFunc: func(context.Context, string, string, []string) (int, error) {
		panic("judge must not be called after an earlier check fails")
	}}
	got, err := Grade(ctx, "q", rec, rules, panicJudge)
	if err != nil {
		t.Fatalf("Grade: %v", err)
	}
	if got[0].Pass {
		t.Error("expected FAIL from the regex check")
	}
}

func TestLoadCorpus(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "corpus.yaml")
	yaml := `probes:
  - id: money_recall
    prompt: "How are monetary amounts stored?"
    rules:
      - id: money_as_string
        severity: critical
        checks:
          - text_matches: "(?i)string"
      - id: mentions_drift
        severity: high
        checks:
          - text_matches: "(?i)drift"
`
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	probes, err := LoadCorpus(path)
	if err != nil {
		t.Fatalf("LoadCorpus: %v", err)
	}
	if len(probes) != 1 {
		t.Fatalf("expected 1 probe, got %d", len(probes))
	}
	p := probes[0]
	if p.ID != "money_recall" || p.Prompt == "" || len(p.Rules) != 2 {
		t.Errorf("unexpected probe: %+v", p)
	}
	if p.Rules[0].Severity != Critical || p.Rules[1].Severity != High {
		t.Errorf("severities not parsed: %+v", p.Rules)
	}
	if len(p.Rules[0].Checks) != 1 {
		t.Errorf("checks not loaded: %+v", p.Rules[0].Checks)
	}
	rec := &parser.RunRecord{FinalText: "Stored as strings, prevents drift."}
	ok, err := p.Rules[0].Checks[0].Eval(ctx, EvalInput{Record: rec})
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if !ok {
		t.Error("expected compiled check to match")
	}
}

func TestLoadCorpus_JudgeRubric(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "corpus.yaml")
	yaml := `probes:
  - id: trace_bet
    prompt: "Trace a bet placement from API to ledger."
    rules:
      - id: names_services
        severity: high
        checks:
          - judge_rubric:
              facts:
                - "names the ledger service"
                - "mentions the gRPC call"
              pass_score: 60
`
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	probes, err := LoadCorpus(path)
	if err != nil {
		t.Fatalf("LoadCorpus: %v", err)
	}
	jr, ok := probes[0].Rules[0].Checks[0].(*JudgeRubric)
	if !ok {
		t.Fatalf("expected *JudgeRubric, got %T", probes[0].Rules[0].Checks[0])
	}
	if len(jr.Facts) != 2 || jr.PassScore != 60 {
		t.Errorf("unexpected rubric: %+v", jr)
	}
}

func TestLoadCorpus_JudgeRubric_Validation(t *testing.T) {
	cases := map[string]string{
		"missing pass_score": `
              facts: ["a"]`,
		"pass_score zero": `
              facts: ["a"]
              pass_score: 0`,
		"pass_score over 100": `
              facts: ["a"]
              pass_score: 101`,
		"empty facts": `
              facts: []
              pass_score: 50`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			yaml := `probes:
  - id: p
    prompt: "x"
    rules:
      - id: r
        severity: high
        checks:
          - judge_rubric:` + body + "\n"
			dir := t.TempDir()
			path := filepath.Join(dir, "c.yaml")
			if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadCorpus(path); err == nil {
				t.Errorf("expected load error for %q", name)
			}
		})
	}
}

func TestLoadCorpus_CheckShapeErrors(t *testing.T) {
	cases := map[string]string{
		"both kinds set": `          - text_matches: "x"
            judge_rubric:
              facts: ["a"]
              pass_score: 50`,
		"empty check":  `          - {}`,
		"unknown kind": `          - bash_call_matches: "docker"`,
		"empty regex":  `          - text_matches: ""`,
	}
	for name, checks := range cases {
		t.Run(name, func(t *testing.T) {
			yaml := `probes:
  - id: p
    prompt: "x"
    rules:
      - id: r
        severity: high
        checks:
` + checks + "\n"
			dir := t.TempDir()
			path := filepath.Join(dir, "c.yaml")
			if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadCorpus(path); err == nil {
				t.Errorf("expected load error for %q", name)
			}
		})
	}
}

func TestLoadCorpus_BadSeverity(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "corpus.yaml")
	yaml := `probes:
  - id: p1
    prompt: "x"
    rules:
      - id: r1
        severity: bogus
        checks:
          - text_matches: ".*"
`
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadCorpus(path); err == nil {
		t.Error("expected error on invalid severity")
	}
}

func TestLoadCorpus_BadRegex(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "corpus.yaml")
	yaml := `probes:
  - id: p1
    prompt: "x"
    rules:
      - id: r1
        severity: critical
        checks:
          - text_matches: "[unclosed"
`
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadCorpus(path); err == nil {
		t.Error("expected error on bad regex")
	}
}

// TestLoadCorpus_ShippedCorpora guards the schema migration: the real shipped
// corpora must still load, and every check must remain a regex (text_matches).
func TestLoadCorpus_ShippedCorpora(t *testing.T) {
	for _, path := range []string{
		"../../.benchmark/corpus/self.yaml",
		"../../examples/corpus/l1-smoke.yaml",
	} {
		t.Run(filepath.Base(path), func(t *testing.T) {
			probes, err := LoadCorpus(path)
			if err != nil {
				t.Fatalf("LoadCorpus(%s): %v", path, err)
			}
			if len(probes) == 0 {
				t.Fatalf("%s: no probes loaded", path)
			}
			for _, p := range probes {
				for _, r := range p.Rules {
					for _, c := range r.Checks {
						if _, ok := c.(*TextMatches); !ok {
							t.Errorf("%s probe %s rule %s: expected *TextMatches, got %T", path, p.ID, r.ID, c)
						}
					}
				}
			}
		})
	}
}

// TestLoadCorpus_L2Example loads the shipped L2 example and confirms it carries
// a judge-backed rule (so NeedsJudge would require --level l2).
func TestLoadCorpus_L2Example(t *testing.T) {
	probes, err := LoadCorpus("../../examples/corpus/l2-smoke.yaml")
	if err != nil {
		t.Fatalf("LoadCorpus: %v", err)
	}
	if !NeedsJudge(probes) {
		t.Error("l2 example should need a judge")
	}
	if _, ok := probes[0].Rules[0].Checks[0].(*JudgeRubric); !ok {
		t.Errorf("expected a *JudgeRubric check, got %T", probes[0].Rules[0].Checks[0])
	}
	var openEnded int
	for _, p := range probes {
		if p.OpenEnded() {
			openEnded++
		}
	}
	if openEnded != 1 {
		t.Errorf("expected exactly 1 open-ended probe in the L2 example, got %d", openEnded)
	}
}

// TestLoadCorpus_PlanExample loads the shipped plan example and confirms it
// carries a plan probe, so NeedsJudge requires a judge (run via --level l2).
func TestLoadCorpus_PlanExample(t *testing.T) {
	probes, err := LoadCorpus("../../examples/corpus/plan-smoke.yaml")
	if err != nil {
		t.Fatalf("LoadCorpus: %v", err)
	}
	if !NeedsJudge(probes) {
		t.Error("plan example should need a judge (comparative preference)")
	}
	var plan int
	for _, p := range probes {
		if p.Kind == Plan {
			plan++
		}
		if len(p.Rules) != 0 {
			t.Errorf("plan probe %s must have no rules: %+v", p.ID, p.Rules)
		}
	}
	if plan != 1 {
		t.Errorf("expected exactly 1 plan probe in the plan example, got %d", plan)
	}
}

func TestLoadCorpus_KindValidation(t *testing.T) {
	cases := map[string]string{
		"open_ended with rules": `probes:
  - id: p
    kind: open_ended
    prompt: "x"
    rules:
      - id: r
        severity: high
        checks:
          - text_matches: "x"`,
		"rule_based with no rules": `probes:
  - id: p
    prompt: "x"`,
		"plan with rules": `probes:
  - id: p
    kind: plan
    prompt: "x"
    rules:
      - id: r
        severity: high
        checks:
          - text_matches: "x"`,
		"bad kind": `probes:
  - id: p
    kind: bogus
    prompt: "x"`,
		"missing prompt": `probes:
  - id: p
    kind: open_ended`,
	}
	for name, yaml := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "c.yaml")
			if err := os.WriteFile(path, []byte(yaml+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadCorpus(path); err == nil {
				t.Errorf("expected load error for %q", name)
			}
		})
	}
}

func TestLoadCorpus_OpenEnded(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "c.yaml")
	yaml := `probes:
  - id: design
    kind: open_ended
    prompt: "What are the tradeoffs?"
`
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	probes, err := LoadCorpus(path)
	if err != nil {
		t.Fatalf("LoadCorpus: %v", err)
	}
	if !probes[0].OpenEnded() || len(probes[0].Rules) != 0 {
		t.Errorf("expected an open-ended, rule-less probe: %+v", probes[0])
	}
}

func TestLoadCorpus_Plan(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "c.yaml")
	yaml := `probes:
  - id: rate_limit_plan
    kind: plan
    prompt: "Plan how to add rate limiting to the API."
`
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	probes, err := LoadCorpus(path)
	if err != nil {
		t.Fatalf("LoadCorpus: %v", err)
	}
	p := probes[0]
	if p.Kind != Plan || !p.Comparative() || p.OpenEnded() || len(p.Rules) != 0 {
		t.Errorf("expected a comparative, rule-less plan probe: %+v", p)
	}
	if !NeedsJudge(probes) {
		t.Error("plan corpus should need a judge (comparative preference)")
	}
}

func TestNeedsJudge(t *testing.T) {
	regexOnly := []Probe{{Rules: []Rule{{Checks: []Check{&TextMatches{Pattern: regexp.MustCompile("x")}}}}}}
	if NeedsJudge(regexOnly) {
		t.Error("regex-only corpus should not need a judge")
	}
	withJudge := []Probe{{Rules: []Rule{{Checks: []Check{&JudgeRubric{Facts: []string{"a"}, PassScore: 50}}}}}}
	if !NeedsJudge(withJudge) {
		t.Error("corpus with a judge_rubric check should need a judge")
	}
}
