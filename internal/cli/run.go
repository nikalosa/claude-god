package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/nikalosa/claude-god/internal/aggregator"
	"github.com/nikalosa/claude-god/internal/dsl"
	"github.com/nikalosa/claude-god/internal/harness"
	"github.com/nikalosa/claude-god/internal/report"
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
		if flagLevel != "l1" {
			return fmt.Errorf("only --level l1 is supported in this slice (got %q)", flagLevel)
		}
		if flagCorpus == "" {
			return fmt.Errorf("--corpus is required")
		}
		if flagSamples < 1 {
			return fmt.Errorf("--samples must be >= 1")
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

		ctx := context.Background()
		aggs := make([]aggregator.AggregatedOutcome, 0, len(probes))
		for _, probe := range probes {
			beforeRuns, err := runN(ctx, target, flagBefore, probe, flagSamples)
			if err != nil {
				return fmt.Errorf("probe %s before: %w", probe.ID, err)
			}
			afterRuns, err := runN(ctx, target, flagAfter, probe, flagSamples)
			if err != nil {
				return fmt.Errorf("probe %s after: %w", probe.ID, err)
			}
			aggs = append(aggs, aggregator.Aggregate(aggregator.ProbeOutcome{
				ProbeID: probe.ID,
				Before:  aggregator.EnvOutcome{Runs: beforeRuns},
				After:   aggregator.EnvOutcome{Runs: afterRuns},
			}))
		}

		verdicts := aggregator.Compare(aggs)
		deltas := aggregator.ComputeDeltas(aggs)
		fmt.Println(report.RenderMarkdown(verdicts, deltas))

		if aggregator.HasCriticalRegression(verdicts) {
			fmt.Fprintln(os.Stderr, "FAIL: critical regression detected")
			os.Exit(1)
		}
		return nil
	},
}

func runN(ctx context.Context, target, branch string, probe dsl.Probe, n int) ([]aggregator.Run, error) {
	runs := make([]aggregator.Run, 0, n)
	for i := 0; i < n; i++ {
		fmt.Fprintf(os.Stderr, "probe %s: sample %d/%d on %s\n", probe.ID, i+1, n, branch)
		res, err := harness.Run(ctx, harness.Opts{
			TargetRepo:    target,
			Branch:        branch,
			Prompt:        probe.Prompt,
			NoMemSnapshot: flagNoMemSnapshot,
		})
		if err != nil {
			return nil, err
		}
		runs = append(runs, aggregator.Run{
			Record:  res.Record,
			Results: dsl.Grade(res.Record, probe.Rules),
		})
	}
	return runs, nil
}

func init() {
	f := runCmd.Flags()
	f.StringVar(&flagLevel, "level", "l1", "comma-separated tiers to run (l1,l2,l3,l4)")
	f.StringVar(&flagTarget, "target", ".", "path to the target repo under test")
	f.StringVar(&flagCorpus, "corpus", "", "path to the probe corpus YAML file")
	f.StringVar(&flagBefore, "before", "validator/before", "branch holding the pre-restructure baseline")
	f.StringVar(&flagAfter, "after", "validator/after", "branch holding the post-restructure config under test")
	f.IntVar(&flagSamples, "samples", 3, "samples per environment (N=3 by default; adaptive N=5 deferred)")
	f.BoolVar(&flagNoMemSnapshot, "no-memory-snapshot", false, "skip pinning project memory into the run")
}
