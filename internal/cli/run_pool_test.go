package cli

import (
	"context"
	"reflect"
	"regexp"
	"testing"

	"github.com/nikalosa/claude-god/internal/aggregator"
	"github.com/nikalosa/claude-god/internal/dsl"
	"github.com/nikalosa/claude-god/internal/parser"
)

// fakeRun is a deterministic runFunc: its record is a pure function of
// (branch, prompt), so the only thing that can vary between concurrency levels
// is the pool's own scheduling — which must not change the result.
func fakeRun(_ context.Context, branch, prompt string) (*parser.RunRecord, error) {
	pass := (prompt == "A" && branch == "before") || (prompt == "B" && branch == "after")
	text := "nope"
	if pass {
		text = "PASS"
	}
	return &parser.RunRecord{
		FinalText:  text,
		TotalCost:  0.01,
		Timing:     parser.Timing{DurationMs: 100},
		Usage:      parser.Usage{InputTokens: 10, OutputTokens: 5},
		ModelUsage: map[string]parser.ModelUsage{"m": {InputTokens: 10, OutputTokens: 5, CostUSD: 0.01}},
	}, nil
}

func poolTestProbes() []dsl.Probe {
	mk := func(id, prompt string) dsl.Probe {
		return dsl.Probe{ID: id, Prompt: prompt, Kind: dsl.RuleBased, Rules: []dsl.Rule{{
			ID: "r", Severity: dsl.Critical, Checks: []dsl.Check{&dsl.TextMatches{Pattern: regexp.MustCompile("PASS")}},
		}}}
	}
	return []dsl.Probe{mk("A", "A"), mk("B", "B")}
}

// TestRunBenchmark_DeterministicAcrossConcurrency is the core guarantee: the
// pool is a scheduling detail, so verdicts and Numbers must be identical at
// --concurrency 1 and 8 for the same inputs. Run with -race for the indexed
// writes.
func TestRunBenchmark_DeterministicAcrossConcurrency(t *testing.T) {
	probes := poolTestProbes()
	ctx := context.Background()

	v1, p1, d1, err := runBenchmark(ctx, probes, "before", "after", 3, 1, fakeRun, nil)
	if err != nil {
		t.Fatalf("concurrency 1: %v", err)
	}
	v8, p8, d8, err := runBenchmark(ctx, probes, "before", "after", 3, 8, fakeRun, nil)
	if err != nil {
		t.Fatalf("concurrency 8: %v", err)
	}

	if !reflect.DeepEqual(v1, v8) {
		t.Errorf("verdicts differ by concurrency:\n c1=%+v\n c8=%+v", v1, v8)
	}
	if !reflect.DeepEqual(d1, d8) {
		t.Errorf("deltas differ by concurrency:\n c1=%+v\n c8=%+v", d1, d8)
	}
	if !reflect.DeepEqual(p1, p8) {
		t.Errorf("preferences differ by concurrency")
	}

	// Guard against a vacuous pass: the fixture must actually produce a flip in
	// each direction, so the determinism check is grading something real.
	var reg, newp int
	for _, v := range v1 {
		switch v.Status {
		case aggregator.Regression:
			reg++
		case aggregator.NewPass:
			newp++
		}
	}
	if reg == 0 || newp == 0 {
		t.Fatalf("fixture is vacuous: want a regression and a new pass, got reg=%d newp=%d (%+v)", reg, newp, v1)
	}
}
