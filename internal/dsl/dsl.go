package dsl

import (
	"context"
	"fmt"
	"os"
	"regexp"

	"gopkg.in/yaml.v3"

	"github.com/nikalosa/claude-god/internal/judge"
	"github.com/nikalosa/claude-god/internal/parser"
)

type Severity string

const (
	Critical Severity = "critical"
	High     Severity = "high"
	Medium   Severity = "medium"
)

type ProbeKind string

const (
	RuleBased ProbeKind = "rule_based"
	OpenEnded ProbeKind = "open_ended"
	Plan      ProbeKind = "plan"
)

type Probe struct {
	ID     string
	Prompt string
	Kind   ProbeKind
	Rules  []Rule
}

func (p Probe) OpenEnded() bool { return p.Kind == OpenEnded }

func (p Probe) Comparative() bool { return p.Kind == OpenEnded || p.Kind == Plan }

type Rule struct {
	ID       string
	Severity Severity
	Checks   []Check
}

type EvalInput struct {
	Prompt string
	Record *parser.RunRecord
	Judge  judge.Judge
}

type Check interface {
	Eval(ctx context.Context, in EvalInput) (bool, error)
	String() string
}

type TextMatches struct {
	Pattern *regexp.Regexp
}

func (c *TextMatches) Eval(_ context.Context, in EvalInput) (bool, error) {
	return c.Pattern.MatchString(in.Record.FinalText), nil
}

func (c *TextMatches) String() string { return "text_matches:" + c.Pattern.String() }

type JudgeRubric struct {
	Facts     []string
	PassScore int
}

func (c *JudgeRubric) Eval(ctx context.Context, in EvalInput) (bool, error) {
	if in.Judge == nil {
		return false, fmt.Errorf("judge_rubric check requires a judge (run with --judge)")
	}
	score, err := in.Judge.Score(ctx, in.Prompt, in.Record.FinalText, c.Facts)
	if err != nil {
		return false, err
	}
	return score >= c.PassScore, nil
}

func (c *JudgeRubric) String() string {
	return fmt.Sprintf("judge_rubric(facts=%d,pass=%d)", len(c.Facts), c.PassScore)
}

type RuleResult struct {
	RuleID   string
	Severity Severity
	Pass     bool
}

func Grade(ctx context.Context, prompt string, rec *parser.RunRecord, rules []Rule, j judge.Judge) ([]RuleResult, error) {
	out := make([]RuleResult, 0, len(rules))
	in := EvalInput{Prompt: prompt, Record: rec, Judge: j}
	for _, r := range rules {
		pass := true
		for _, c := range r.Checks {
			ok, err := c.Eval(ctx, in)
			if err != nil {
				return nil, fmt.Errorf("rule %s check %s: %w", r.ID, c.String(), err)
			}
			if !ok {
				pass = false
				break
			}
		}
		out = append(out, RuleResult{RuleID: r.ID, Severity: r.Severity, Pass: pass})
	}
	return out, nil
}

func NeedsJudge(probes []Probe) bool {
	for _, p := range probes {
		if p.Comparative() {
			return true
		}
		if NeedsRubricJudge([]Probe{p}) {
			return true
		}
	}
	return false
}

func NeedsRubricJudge(probes []Probe) bool {
	for _, p := range probes {
		for _, r := range p.Rules {
			for _, c := range r.Checks {
				if _, ok := c.(*JudgeRubric); ok {
					return true
				}
			}
		}
	}
	return false
}

type rawCorpus struct {
	Probes []rawProbe `yaml:"probes"`
}

type rawProbe struct {
	ID     string    `yaml:"id"`
	Prompt string    `yaml:"prompt"`
	Kind   string    `yaml:"kind"`
	Rules  []rawRule `yaml:"rules"`
}

type rawRule struct {
	ID       string                 `yaml:"id"`
	Severity string                 `yaml:"severity"`
	Checks   []map[string]yaml.Node `yaml:"checks"`
}

type rawJudgeRubric struct {
	Facts     []string `yaml:"facts"`
	PassScore *int     `yaml:"pass_score"`
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
		if rp.Prompt == "" {
			return nil, fmt.Errorf("probe %s: prompt is required", rp.ID)
		}
		kind, err := parseKind(rp.Kind)
		if err != nil {
			return nil, fmt.Errorf("probe %s: %w", rp.ID, err)
		}
		p := Probe{ID: rp.ID, Prompt: rp.Prompt, Kind: kind}
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
		switch kind {
		case OpenEnded:
			if len(p.Rules) > 0 {
				return nil, fmt.Errorf("probe %s: open_ended probe must have no rules", rp.ID)
			}
		case Plan:
			if len(p.Rules) > 0 {
				return nil, fmt.Errorf("probe %s: plan probe must have no rules", rp.ID)
			}
		case RuleBased:
			if len(p.Rules) == 0 {
				return nil, fmt.Errorf("probe %s: rule_based probe needs >=1 rule (use kind: open_ended for a preference probe)", rp.ID)
			}
		}
		probes = append(probes, p)
	}
	return probes, nil
}

func parseKind(s string) (ProbeKind, error) {
	switch s {
	case "", string(RuleBased):
		return RuleBased, nil
	case string(OpenEnded):
		return OpenEnded, nil
	case string(Plan):
		return Plan, nil
	default:
		return "", fmt.Errorf("invalid kind %q (want rule_based|open_ended|plan)", s)
	}
}

func parseSeverity(s string) (Severity, error) {
	switch Severity(s) {
	case Critical, High, Medium:
		return Severity(s), nil
	default:
		return "", fmt.Errorf("invalid severity %q (want critical|high|medium)", s)
	}
}

func buildCheck(raw map[string]yaml.Node) (Check, error) {
	if len(raw) != 1 {
		return nil, fmt.Errorf("check must have exactly one kind, got %d", len(raw))
	}
	for k, node := range raw {
		switch k {
		case "text_matches":
			var pat string
			if err := node.Decode(&pat); err != nil {
				return nil, fmt.Errorf("text_matches: %w", err)
			}
			if pat == "" {
				return nil, fmt.Errorf("text_matches: empty pattern")
			}
			re, err := regexp.Compile(pat)
			if err != nil {
				return nil, fmt.Errorf("text_matches %q: %w", pat, err)
			}
			return &TextMatches{Pattern: re}, nil
		case "judge_rubric":
			var jr rawJudgeRubric
			if err := node.Decode(&jr); err != nil {
				return nil, fmt.Errorf("judge_rubric: %w", err)
			}
			if len(jr.Facts) == 0 {
				return nil, fmt.Errorf("judge_rubric: needs at least one fact")
			}
			if jr.PassScore == nil {
				return nil, fmt.Errorf("judge_rubric: pass_score is required (1..100)")
			}
			if *jr.PassScore < 1 || *jr.PassScore > 100 {
				return nil, fmt.Errorf("judge_rubric: pass_score %d out of range (1..100)", *jr.PassScore)
			}
			return &JudgeRubric{Facts: jr.Facts, PassScore: *jr.PassScore}, nil
		default:
			return nil, fmt.Errorf("unknown check kind %q", k)
		}
	}
	return nil, fmt.Errorf("unreachable")
}
