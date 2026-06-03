package cli

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/nikalosa/claude-god/internal/dsl"
	"github.com/nikalosa/claude-god/internal/report"
)

var (
	flagCalLevel       string
	flagCalTarget      string
	flagCalCorpus      string
	flagCalBranch      string
	flagCalSamples     int
	flagCalConcurrency int
	flagCalNoMem       bool
)

var calibrateCmd = &cobra.Command{
	Use:   "calibrate",
	Short: "Measure the noise floor by running a corpus Before-vs-Before",
	Long: `calibrate runs the dataset with the same Environment on both sides and
reports the rules that come out flaky (non-stable on identical input) plus the
Numbers spread — the false-positive rate a real Before-vs-After run stands on.
Tighten or drop flaky rules before trusting a comparison. Never gates.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		levels, err := parseLevels(flagCalLevel)
		if err != nil {
			return err
		}
		if flagCalCorpus == "" {
			return fmt.Errorf("--corpus is required")
		}
		if err := validateSamples(flagCalSamples); err != nil {
			return err
		}
		if err := validateConcurrency(flagCalConcurrency); err != nil {
			return err
		}
		target, err := filepath.Abs(flagCalTarget)
		if err != nil {
			return fmt.Errorf("resolve --target: %w", err)
		}

		probes, err := dsl.LoadCorpus(flagCalCorpus)
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
		run := harnessRun(target, flagCalNoMem)
		verdicts, _, deltas, err := runBenchmark(ctx, probes, flagCalBranch, flagCalBranch, flagCalSamples, flagCalConcurrency, run, j)
		if err != nil {
			return err
		}
		fmt.Println(report.RenderCalibration(verdicts, deltas, flagCalConcurrency))
		return nil
	},
}

func init() {
	f := calibrateCmd.Flags()
	f.StringVar(&flagCalLevel, "level", "l1", "comma-separated tiers to run (l1, l2)")
	f.StringVar(&flagCalTarget, "target", ".", "path to the target repo under test")
	f.StringVar(&flagCalCorpus, "corpus", "", "path to the probe corpus YAML file")
	f.StringVar(&flagCalBranch, "branch", "main", "the Environment branch to calibrate (run Before-vs-Before)")
	f.IntVar(&flagCalSamples, "samples", 3, "samples per environment (odd N)")
	f.IntVar(&flagCalConcurrency, "concurrency", 8, "max runs in flight (>=1; Duration is advisory above 1)")
	f.BoolVar(&flagCalNoMem, "no-memory-snapshot", false, "skip pinning project memory into the run")
	rootCmd.AddCommand(calibrateCmd)
}
