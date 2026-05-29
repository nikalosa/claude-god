package dsl

import (
	"fmt"
	"os"
	"regexp"

	"gopkg.in/yaml.v3"

	"github.com/nikalosa/claude-god/internal/parser"
)

type Severity string

const (
	Critical Severity = "critical"
	High     Severity = "high"
	Medium   Severity = "medium"
)

type Probe struct {
	ID     string
	Prompt string
	Rules  []Rule
}

type Rule struct {
	ID       string
	Severity Severity
	Checks   []Check
}

type Check interface {
	Eval(rec *parser.RunRecord) bool
	String() string
}

type TextMatches struct {
	Pattern *regexp.Regexp
}

func (c *TextMatches) Eval(rec *parser.RunRecord) bool {
	return c.Pattern.MatchString(rec.FinalText)
}

func (c *TextMatches) String() string { return "text_matches:" + c.Pattern.String() }

type RuleResult struct {
	RuleID   string
	Severity Severity
	Pass     bool
}

func Grade(rec *parser.RunRecord, rules []Rule) []RuleResult {
	out := make([]RuleResult, 0, len(rules))
	for _, r := range rules {
		pass := true
		for _, c := range r.Checks {
			if !c.Eval(rec) {
				pass = false
				break
			}
		}
		out = append(out, RuleResult{RuleID: r.ID, Severity: r.Severity, Pass: pass})
	}
	return out
}

type rawCorpus struct {
	Probes []rawProbe `yaml:"probes"`
}

type rawProbe struct {
	ID     string    `yaml:"id"`
	Prompt string    `yaml:"prompt"`
	Rules  []rawRule `yaml:"rules"`
}

type rawRule struct {
	ID       string              `yaml:"id"`
	Severity string              `yaml:"severity"`
	Checks   []map[string]string `yaml:"checks"`
}

func LoadCorpus(path string) ([]Probe, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read corpus: %w", err)
	}
	var raw rawCorpus
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse corpus: %w", err)
	}
	probes := make([]Probe, 0, len(raw.Probes))
	for _, rp := range raw.Probes {
		if rp.ID == "" {
			return nil, fmt.Errorf("probe missing id")
		}
		p := Probe{ID: rp.ID, Prompt: rp.Prompt}
		for _, rr := range rp.Rules {
			if rr.ID == "" {
				return nil, fmt.Errorf("probe %s: rule missing id", rp.ID)
			}
			sev, err := parseSeverity(rr.Severity)
			if err != nil {
				return nil, fmt.Errorf("probe %s rule %s: %w", rp.ID, rr.ID, err)
			}
			rule := Rule{ID: rr.ID, Severity: sev}
			for _, rc := range rr.Checks {
				c, err := buildCheck(rc)
				if err != nil {
					return nil, fmt.Errorf("probe %s rule %s: %w", rp.ID, rr.ID, err)
				}
				rule.Checks = append(rule.Checks, c)
			}
			p.Rules = append(p.Rules, rule)
		}
		probes = append(probes, p)
	}
	return probes, nil
}

func parseSeverity(s string) (Severity, error) {
	switch Severity(s) {
	case Critical, High, Medium:
		return Severity(s), nil
	default:
		return "", fmt.Errorf("invalid severity %q (want critical|high|medium)", s)
	}
}

func buildCheck(raw map[string]string) (Check, error) {
	if len(raw) != 1 {
		return nil, fmt.Errorf("check must have exactly one key, got %d", len(raw))
	}
	for k, v := range raw {
		switch k {
		case "text_matches":
			re, err := regexp.Compile(v)
			if err != nil {
				return nil, fmt.Errorf("text_matches %q: %w", v, err)
			}
			return &TextMatches{Pattern: re}, nil
		default:
			return nil, fmt.Errorf("unknown check kind %q", k)
		}
	}
	return nil, fmt.Errorf("unreachable")
}
