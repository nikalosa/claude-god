package dsl

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/nikalosa/claude-god/internal/parser"
)

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
	got := Grade(rec, rules)
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
	got := Grade(rec, rules)
	if got[0].Pass {
		t.Errorf("expected FAIL when one of multiple checks fails")
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
	if !p.Rules[0].Checks[0].Eval(rec) {
		t.Error("expected compiled check to match")
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
