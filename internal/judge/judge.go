package judge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
)

// Outcome of a comparison between the Before and After answers.
type Outcome string

const (
	Tie          Outcome = "tie"
	BeforeBetter Outcome = "before_better"
	AfterBetter  Outcome = "after_better"
)

// Preference is the result of comparing two answers head-to-head across three
// dimensions. Report-only: it never carries a PASS/FAIL or severity.
type Preference struct {
	Outcome    Outcome // overall, surviving both orderings
	Concise    Outcome
	Exhaustive Outcome
	Direct     Outcome
	Reasoning  string
}

// Judge grades Claude answers. Implementations stay neutral (no target
// Environment) and isolated from the deterministic pattern path.
type Judge interface {
	// Score grades one answer against a rubric (facts it should contain),
	// returning 0..100 (percent of facts present). The N=3 median is taken at
	// the call site, not here.
	Score(ctx context.Context, question, answer string, rubric []string) (int, error)
	// Prefer compares Before vs After across concise/exhaustive/direct, running
	// both orderings; a side wins only if it wins in both, else Tie.
	Prefer(ctx context.Context, question, before, after string) (Preference, error)
}

type client struct{ backend Backend }

// New returns a Judge backed by b.
func New(b Backend) Judge { return &client{backend: b} }

type scoreVerdict struct {
	Facts []struct {
		Index   int  `json:"index"`
		Present bool `json:"present"`
	} `json:"facts"`
}

func (c *client) Score(ctx context.Context, question, answer string, rubric []string) (int, error) {
	if len(rubric) == 0 {
		return 0, errors.New("judge: empty rubric")
	}
	raw, err := c.backend.Ask(ctx, buildScorePrompt(question, answer, rubric))
	if err != nil {
		return 0, err
	}
	jb, err := extractJSON(raw)
	if err != nil {
		return 0, fmt.Errorf("judge: score: %w", err)
	}
	var v scoreVerdict
	if err := json.Unmarshal(jb, &v); err != nil {
		return 0, fmt.Errorf("judge: score verdict: %w", err)
	}
	present := 0
	for _, f := range v.Facts {
		if f.Present {
			present++
		}
	}
	total := len(rubric) // authoritative denominator; the rubric we asked about
	if present > total {
		present = total
	}
	return int(math.Round(float64(present) / float64(total) * 100)), nil
}

type prefVerdict struct {
	Winner     int    `json:"winner"`
	Concise    int    `json:"concise"`
	Exhaustive int    `json:"exhaustive"`
	Direct     int    `json:"direct"`
	Reasoning  string `json:"reasoning"`
}

func (c *client) Prefer(ctx context.Context, question, before, after string) (Preference, error) {
	a, err := c.compareOnce(ctx, question, before, after) // pos1=before, pos2=after
	if err != nil {
		return Preference{}, err
	}
	b, err := c.compareOnce(ctx, question, after, before) // pos1=after, pos2=before
	if err != nil {
		return Preference{}, err
	}
	// A side wins a dimension only if it wins in BOTH orderings; the positional
	// meaning of winner 1/2 is swapped between orderings.
	dim := func(av, bv int) Outcome {
		return combine(
			mapWinner(av, BeforeBetter, AfterBetter),
			mapWinner(bv, AfterBetter, BeforeBetter),
		)
	}
	return Preference{
		Outcome:    dim(a.Winner, b.Winner),
		Concise:    dim(a.Concise, b.Concise),
		Exhaustive: dim(a.Exhaustive, b.Exhaustive),
		Direct:     dim(a.Direct, b.Direct),
		Reasoning:  "ordering A: " + a.Reasoning + " | ordering B: " + b.Reasoning,
	}, nil
}

func (c *client) compareOnce(ctx context.Context, question, pos1, pos2 string) (prefVerdict, error) {
	raw, err := c.backend.Ask(ctx, buildPreferencePrompt(question, pos1, pos2))
	if err != nil {
		return prefVerdict{}, err
	}
	jb, err := extractJSON(raw)
	if err != nil {
		return prefVerdict{}, fmt.Errorf("judge: prefer: %w", err)
	}
	var v prefVerdict
	if err := json.Unmarshal(jb, &v); err != nil {
		return prefVerdict{}, fmt.Errorf("judge: prefer verdict: %w", err)
	}
	return v, nil
}

func mapWinner(winner int, pos1, pos2 Outcome) Outcome {
	switch winner {
	case 1:
		return pos1
	case 2:
		return pos2
	default:
		return Tie
	}
}

func combine(a, b Outcome) Outcome {
	if a == b && a != Tie {
		return a
	}
	return Tie
}

func buildScorePrompt(question, answer string, rubric []string) string {
	var b strings.Builder
	b.WriteString("You are grading an answer to a question against a checklist of facts. ")
	b.WriteString("For each numbered fact, decide whether the ANSWER states or clearly conveys it.\n\n")
	b.WriteString("QUESTION:\n")
	b.WriteString(question)
	b.WriteString("\n\nANSWER:\n")
	b.WriteString(answer)
	b.WriteString("\n\nCHECKLIST:\n")
	for i, f := range rubric {
		fmt.Fprintf(&b, "%d. %s\n", i+1, f)
	}
	b.WriteString("\nRespond with ONLY a JSON object, no prose and no code fence:\n")
	b.WriteString(`{"facts":[{"index":1,"present":true},{"index":2,"present":false}]}`)
	b.WriteString("\nInclude exactly one entry per checklist item, in order.")
	return b.String()
}

func buildPreferencePrompt(question, ans1, ans2 string) string {
	var b strings.Builder
	b.WriteString("You are comparing two answers to the same question, deciding which is better ")
	b.WriteString("for a software developer to read. Judge on three dimensions: conciseness, ")
	b.WriteString("exhaustiveness, directness.\n\n")
	b.WriteString("QUESTION:\n")
	b.WriteString(question)
	b.WriteString("\n\nANSWER 1:\n")
	b.WriteString(ans1)
	b.WriteString("\n\nANSWER 2:\n")
	b.WriteString(ans2)
	b.WriteString("\n\nFor each dimension and overall, pick the better answer: 1, 2, or 0 for a tie. ")
	b.WriteString("Respond with ONLY a JSON object, no prose and no code fence:\n")
	b.WriteString(`{"winner":1,"concise":1,"exhaustive":2,"direct":0,"reasoning":"<one sentence>"}`)
	return b.String()
}
