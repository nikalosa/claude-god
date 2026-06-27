package runner

import (
	"context"
	"fmt"
	"os"

	"github.com/nikalosa/claude-god/internal/aggregator"
	"github.com/nikalosa/claude-god/internal/dsl"
	"github.com/nikalosa/claude-god/internal/judge"
	"github.com/nikalosa/claude-god/internal/parser"
)

type PreferenceResult struct {
	ProbeID    string
	Outcome    judge.Outcome
	Concise    judge.Outcome
	Exhaustive judge.Outcome
	Direct     judge.Outcome
	Reasoning  string
}

func GradeProbe(ctx context.Context, probe dsl.Probe, before, after []*parser.RunRecord, j judge.Judge) (aggregator.AggregatedOutcome, *PreferenceResult, error) {
	beforeRuns, err := gradeRuns(ctx, probe, before, j)
	if err != nil {
		return aggregator.AggregatedOutcome{}, nil, err
	}
	afterRuns, err := gradeRuns(ctx, probe, after, j)
	if err != nil {
		return aggregator.AggregatedOutcome{}, nil, err
	}
	agg := aggregator.Aggregate(aggregator.ProbeOutcome{
		ProbeID: probe.ID,
		Before:  aggregator.EnvOutcome{Runs: beforeRuns},
		After:   aggregator.EnvOutcome{Runs: afterRuns},
	})

	if !probe.Comparative() || len(before) == 0 || len(after) == 0 {
		return agg, nil, nil
	}
	if j == nil {
		return agg, nil, fmt.Errorf("comparative probe %s needs a judge", probe.ID)
	}
	pref, err := j.Prefer(ctx, probe.Prompt, before[0].FinalText, after[0].FinalText)
	if err != nil {

		fmt.Fprintf(os.Stderr, "warning: preference unavailable for %s (judge failed after retries): %v\n", probe.ID, err)
		return agg, nil, nil
	}
	return agg, &PreferenceResult{
		ProbeID:    probe.ID,
		Outcome:    pref.Outcome,
		Concise:    pref.Concise,
		Exhaustive: pref.Exhaustive,
		Direct:     pref.Direct,
		Reasoning:  pref.Reasoning,
	}, nil
}

func gradeRuns(ctx context.Context, probe dsl.Probe, records []*parser.RunRecord, j judge.Judge) ([]aggregator.Run, error) {
	runs := make([]aggregator.Run, 0, len(records))
	for _, rec := range records {
		var results []dsl.RuleResult
		if !probe.Comparative() {
			r, err := dsl.Grade(ctx, probe.Prompt, rec, probe.Rules, j)
			if err != nil {
				return nil, err
			}
			results = r
		}
		runs = append(runs, aggregator.Run{Record: rec, Results: results})
	}
	return runs, nil
}
