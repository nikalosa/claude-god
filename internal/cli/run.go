package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/nikalosa/claude-god/internal/aggregator"
	"github.com/nikalosa/claude-god/internal/dsl"
	"github.com/nikalosa/claude-god/internal/harness"
	"github.com/nikalosa/claude-god/internal/judge"
	"github.com/nikalosa/claude-god/internal/parser"
	"github.com/nikalosa/claude-god/internal/report"
	"github.com/nikalosa/claude-god/internal/runner"
)

var (
	flagLevel         string
	flagTarget        string
	flagCorpus        string
	flagBefore        string
	flagAfter         string
	flagSamples       int
	flagNoMemSnapshot bool
)

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Run the A/B benchmark for the given tiers",
	RunE: func(cmd *cobra.Command, args []string) error {
		levels, err := parseLevels(flagLevel)
		if err != nil {
			return err
		}
		if flagCorpus == "" {
			return fmt.Errorf("--corpus is required")
		}
		if err := validateSamples(flagSamples); err != nil {
			return err
		}
		target, err := filepath.Abs(flagTarget)
		if err != nil {
			return fmt.Errorf("resolve --target: %w", err)
		}

		probes, err := dsl.LoadCorpus(flagCorpus)
		if err != nil {
			return err
		}
		if len(probes) == 0 {
			return fmt.Errorf("corpus has no probes")
		}

		j, err := judgeFor(probes, levels)
		if err != nil {
			return err
		}

		ctx := context.Background()
		verdicts, prefs, deltas, err := runBenchmark(ctx, target, probes, flagBefore, flagAfter, flagSamples, flagNoMemSnapshot, j)
		if err != nil {
			return err
		}
		fmt.Println(report.RenderMarkdown(verdicts, prefs, deltas))

		if aggregator.HasCriticalRegression(verdicts) {
			fmt.Fprintln(os.Stderr, "FAIL: critical regression detected")
			os.Exit(1)
		}
		return nil
	},
}

// parseLevels splits the --level CSV into a tier set, accepting l1/l2 and
// rejecting the not-yet-supported l3/l4 and unknown tokens.
func parseLevels(s string) (map[string]bool, error) {
	set := map[string]bool{}
	for _, tok := range strings.Split(s, ",") {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			continue
		}
		switch tok {
		case "l1", "l2":
			set[tok] = true
		case "l3", "l4":
			return nil, fmt.Errorf("tier %q is not supported in v1 (only l1, l2)", tok)
		default:
			return nil, fmt.Errorf("unknown tier %q (want l1 or l2)", tok)
		}
	}
	if len(set) == 0 {
		return nil, fmt.Errorf("--level is empty")
	}
	return set, nil
}

// validateSamples requires an odd N: the aggregator's majority vote equals
// median-of-scores thresholding (the judge-rubric contract) only for odd N.
func validateSamples(n int) error {
	if n < 1 {
		return fmt.Errorf("--samples must be >= 1")
	}
	if n%2 == 0 {
		return fmt.Errorf("--samples must be odd (got %d) so median == majority vote", n)
	}
	return nil
}

// judgeFor builds a Judge iff the corpus has judge-backed rules, requiring l2 to
// be enabled in that case so a judge check never runs without a judge.
func judgeFor(probes []dsl.Probe, levels map[string]bool) (judge.Judge, error) {
	if !dsl.NeedsJudge(probes) {
		return nil, nil
	}
	if !levels["l2"] {
		return nil, fmt.Errorf("corpus has judge_rubric rules (L2); add l2 to --level (got %q)", flagLevel)
	}
	return judge.New(judge.NewClaudeBackend()), nil
}

// runBenchmark samples every probe on the before and after branches, grades
// each, and returns the verdicts, open-ended preferences, and Numbers. Shared
// by run and calibrate (calibrate passes the same branch on both sides).
func runBenchmark(ctx context.Context, target string, probes []dsl.Probe, before, after string, samples int, noMem bool, j judge.Judge) ([]aggregator.Verdict, []runner.PreferenceResult, aggregator.Deltas, error) {
	aggs := make([]aggregator.AggregatedOutcome, 0, len(probes))
	var prefs []runner.PreferenceResult
	for _, probe := range probes {
		beforeRecs, err := sampleN(ctx, target, before, probe, samples, noMem)
		if err != nil {
			return nil, nil, aggregator.Deltas{}, fmt.Errorf("probe %s before: %w", probe.ID, err)
		}
		afterRecs, err := sampleN(ctx, target, after, probe, samples, noMem)
		if err != nil {
			return nil, nil, aggregator.Deltas{}, fmt.Errorf("probe %s after: %w", probe.ID, err)
		}
		agg, pref, err := runner.GradeProbe(ctx, probe, beforeRecs, afterRecs, j)
		if err != nil {
			return nil, nil, aggregator.Deltas{}, fmt.Errorf("probe %s: %w", probe.ID, err)
		}
		aggs = append(aggs, agg)
		if pref != nil {
			prefs = append(prefs, *pref)
		}
	}
	return aggregator.Compare(aggs), prefs, aggregator.ComputeDeltas(aggs), nil
}

// sampleN runs one probe n times on a branch and collects the run records;
// grading is the runner's job, so the live harness loop stays free of it.
func sampleN(ctx context.Context, target, branch string, probe dsl.Probe, n int, noMem bool) ([]*parser.RunRecord, error) {
	records := make([]*parser.RunRecord, 0, n)
	for i := 0; i < n; i++ {
		fmt.Fprintf(os.Stderr, "probe %s: sample %d/%d on %s\n", probe.ID, i+1, n, branch)
		res, err := harness.Run(ctx, harness.Opts{
			TargetRepo:    target,
			Branch:        branch,
			Prompt:        probe.Prompt,
			NoMemSnapshot: noMem,
		})
		if err != nil {
			return nil, err
		}
		records = append(records, res.Record)
	}
	return records, nil
}

func init() {
	f := runCmd.Flags()
	f.StringVar(&flagLevel, "level", "l1", "comma-separated tiers to run (l1, l2)")
	f.StringVar(&flagTarget, "target", ".", "path to the target repo under test")
	f.StringVar(&flagCorpus, "corpus", "", "path to the probe corpus YAML file")
	f.StringVar(&flagBefore, "before", "validator/before", "branch holding the pre-restructure baseline")
	f.StringVar(&flagAfter, "after", "validator/after", "branch holding the post-restructure config under test")
	f.IntVar(&flagSamples, "samples", 3, "samples per environment (odd N; N=3 by default, adaptive N=5 deferred)")
	f.BoolVar(&flagNoMemSnapshot, "no-memory-snapshot", false, "skip pinning project memory into the run")
}
