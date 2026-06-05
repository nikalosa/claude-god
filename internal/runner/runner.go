// Package runner orchestrates one probe through grading: a rule-based probe is
// graded per-run and aggregated; an open-ended probe skips rule grading and is
// compared head-to-head by the judge. It is the seam where the deterministic
// grading (dsl, aggregator) meets the judge, kept out of the cobra CLI so the
// branch logic stays testable with a stub judge.
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

// PreferenceResult is the report-only outcome of one open-ended probe. It
// carries no severity and no PASS/FAIL and never becomes a Verdict, so it can
// never affect the exit code.
type PreferenceResult struct {
	ProbeID    string
	Outcome    judge.Outcome
	Concise    judge.Outcome
	Exhaustive judge.Outcome
	Direct     judge.Outcome
	Reasoning  string
}

// GradeProbe grades one probe's before/after run records. It always returns an
// AggregatedOutcome (median Numbers, plus per-rule majority vote for rule-based
// probes). For an open-ended probe it additionally compares a representative
// before/after answer (sample 0) via the judge and returns a PreferenceResult;
// otherwise the preference is nil.
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

	if !probe.OpenEnded() || len(before) == 0 || len(after) == 0 {
		return agg, nil, nil
	}
	if j == nil {
		return agg, nil, fmt.Errorf("open-ended probe %s needs a judge", probe.ID)
	}
	pref, err := j.Prefer(ctx, probe.Prompt, before[0].FinalText, after[0].FinalText)
	if err != nil {
		// Preference is report-only. After the backend's own retries, a still-
		// failing judge call must not discard a completed (expensive) run: drop
		// just this probe's preference and keep its Numbers + every other probe.
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

// gradeRuns grades each run of one environment. Open-ended probes carry no
// rules, so they produce empty results but still flow through aggregation for
// their Numbers.
func gradeRuns(ctx context.Context, probe dsl.Probe, records []*parser.RunRecord, j judge.Judge) ([]aggregator.Run, error) {
	runs := make([]aggregator.Run, 0, len(records))
	for _, rec := range records {
		var results []dsl.RuleResult
		if !probe.OpenEnded() {
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
