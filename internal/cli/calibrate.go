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
	flagCalJudge       bool
	flagCalKind        string
	flagCalTarget      string
	flagCalCorpus      string
	flagCalBranch      string
	flagCalMCP         string
	flagCalSamples     int
	flagCalConcurrency int
	flagCalNoMem       bool
	calCacheFlags      cacheFlags
)

var calibrateCmd = &cobra.Command{
	Use:   "calibrate",
	Short: "Measure the noise floor by running a corpus Before-vs-Before",
	Long: `calibrate runs the dataset with the same Environment on both sides and
reports the rules that come out flaky (non-stable on identical input) plus the
Numbers spread — the false-positive rate a real Before-vs-After run stands on.
Tighten or drop flaky rules before trusting a comparison. Never gates.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		kinds, err := parseKinds(flagCalKind)
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
		probes, err = filterByKind(probes, kinds)
		if err != nil {
			return err
		}

		j, err := judgeFor(probes, flagCalJudge)
		if err != nil {
			return err
		}

		ctx := context.Background()
		env := Env{Ref: flagCalBranch, MCPConfig: flagCalMCP}
		mem := memPolicy{noSnapshot: flagCalNoMem}
		store, err := newStore(target, mem, calCacheFlags, flagCalConcurrency)
		if err != nil {
			return err
		}
		run, cleanup, err := sharedRun(ctx, target, mem, calCacheFlags.model, calCacheFlags.effort, env, env)
		if err != nil {
			return err
		}
		defer cleanup()
		// calibrate measures the noise floor, so it always draws fresh (the cache
		// would replay frozen draws and report zero Disagreement); writes still
		// land, growing the Sample pool. This is the role ADR-0016 folds into
		// `assess --no-cache`.
		verdicts, _, aggs, err := runBenchmark(ctx, probes, env, env, flagCalSamples, flagCalConcurrency, run, store, true, j, "")
		if err != nil {
			return err
		}
		fmt.Println(report.RenderCalibration(verdicts, aggs, flagCalConcurrency))
		return nil
	},
}

func init() {
	f := calibrateCmd.Flags()
	f.BoolVar(&flagCalJudge, "judge", false, "build the Judge for open-ended/plan/judge_rubric corpora (adds claude -p calls — extra spend)")
	f.StringVar(&flagCalKind, "kind", allKinds, "probe kinds to run (CSV of rule_based,open_ended,plan)")
	f.StringVar(&flagCalTarget, "target", ".", "path to the target repo under test")
	f.StringVar(&flagCalCorpus, "corpus", "", "path to the probe corpus YAML file")
	f.StringVar(&flagCalBranch, "branch", "main", "the Environment branch to calibrate (run Before-vs-Before)")
	f.StringVar(&flagCalMCP, "mcp", "", "MCP config for the calibrated Environment (a --mcp-config file path or inline JSON)")
	f.IntVar(&flagCalSamples, "samples", 3, "samples per environment (odd N)")
	f.IntVar(&flagCalConcurrency, "concurrency", 8, "max runs in flight (>=1; Duration is advisory above 1)")
	f.BoolVar(&flagCalNoMem, "no-memory-snapshot", false, "skip pinning project memory into the run")
	addCacheFlags(f, &calCacheFlags)
	rootCmd.AddCommand(calibrateCmd)
}
