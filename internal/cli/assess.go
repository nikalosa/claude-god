package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync/atomic"

	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"

	"github.com/nikalosa/claude-god/internal/aggregator"
	"github.com/nikalosa/claude-god/internal/autodetect"
	"github.com/nikalosa/claude-god/internal/cache"
	"github.com/nikalosa/claude-god/internal/dsl"
	"github.com/nikalosa/claude-god/internal/judge"
	"github.com/nikalosa/claude-god/internal/parser"
	"github.com/nikalosa/claude-god/internal/report"
	"github.com/nikalosa/claude-god/internal/runner"
)

var (
	flagAssessJudge       bool
	flagAssessKind        string
	flagAssessTarget      string
	flagAssessCorpus      string
	flagAssessRef         string
	flagAssessMCP         string
	flagAssessSamples     int
	flagAssessConcurrency int
	flagAssessYes         bool
	assessCacheFlags      cacheFlags
)

var assessCmd = &cobra.Command{
	Use:   "assess",
	Short: "Score one Environment against a corpus (no A/B): absolute rule PASS/FAIL + Numbers",
	Long: `assess runs the corpus against a single Environment and prints an absolute
scorecard — each rule PASS/FAIL on its own, plus single-env Numbers (no Δ).

It answers "assess current config with this corpus", where there is no second
config to prefer against. open_ended/plan probes are graded comparatively, so
assess runs them for their Numbers but lists them as not graded — use the A/B
benchmark (the bare command or run) to compare two configs.`,
	RunE: assessRunE,
}

func assessRunE(cmd *cobra.Command, _ []string) error {
	kinds, err := parseKinds(flagAssessKind)
	if err != nil {
		return err
	}
	if err := validateSamples(flagAssessSamples); err != nil {
		return err
	}
	if err := validateConcurrency(flagAssessConcurrency); err != nil {
		return err
	}
	target, err := filepath.Abs(flagAssessTarget)
	if err != nil {
		return fmt.Errorf("resolve --target: %w", err)
	}

	corpusPath, err := discoverCorpus(target, flagAssessCorpus, os.Stdin)
	if err != nil {
		return err
	}

	ctx := cmd.Context()
	ref, desc, volatile, err := autodetect.ResolveOne(ctx, target, flagAssessRef)
	if err != nil {
		return err
	}

	probes, err := dsl.LoadCorpus(corpusPath)
	if err != nil {
		return err
	}
	if len(probes) == 0 {
		return fmt.Errorf("corpus %s has no probes", corpusPath)
	}
	probes, err = filterByKind(probes, kinds)
	if err != nil {
		return err
	}

	j, err := judgeForAssess(probes, flagAssessJudge)
	if err != nil {
		return err
	}

	memSrc, err := memorySourceFor(target)
	if err != nil {
		return err
	}
	mem := memPolicy{source: memSrc}
	store, err := newStore(target, mem, assessCacheFlags, flagAssessConcurrency)
	if err != nil {
		return err
	}
	env := Env{Ref: ref, MCPConfig: flagAssessMCP, Volatile: volatile}

	cached, toRun, err := cachePlan(store, probes, flagAssessSamples, assessCacheFlags.noCache, env)
	if err != nil {
		return err
	}
	printAssessPlan(os.Stderr, desc, corpusPath, probes, flagAssessSamples, flagAssessConcurrency, cached, toRun)
	ok, err := confirm(flagAssessYes, os.Stdin)
	if err != nil {
		return err
	}
	if !ok {
		fmt.Fprintln(os.Stderr, "aborted.")
		return nil
	}

	run, cleanup, err := sharedRun(ctx, target, mem, assessCacheFlags.model, assessCacheFlags.effort, env)
	if err != nil {
		return err
	}
	defer cleanup()

	aggs, err := runSingleEnv(ctx, probes, env, flagAssessSamples, flagAssessConcurrency, run, store, assessCacheFlags.noCache, j)
	if err != nil {
		return err
	}
	fmt.Println(report.RenderAssessment(aggs, desc, flagAssessConcurrency))
	return nil
}

// judgeForAssess builds a Judge iff a rule carries a judge_rubric check. assess
// never runs a Preference comparison, so comparative probes need no judge — only
// an absolute rubric rule does (distinct from judgeFor, which the A/B path uses).
func judgeForAssess(probes []dsl.Probe, judgeOn bool) (judge.Judge, error) {
	if !dsl.NeedsRubricJudge(probes) {
		return nil, nil
	}
	if !judgeOn {
		return nil, fmt.Errorf("corpus has judge_rubric rules; pass --judge to enable the Judge (adds claude -p calls — extra spend)")
	}
	return judge.New(judge.NewClaudeBackend()), nil
}

// runSingleEnv serves every probe's Sample pool on one ref from the Run cache,
// running only the misses, then grades each probe with an empty After side:
// rule_based probes grade absolutely, comparative probes skip the Preference
// comparison (GradeProbe needs two answers) and keep only their Numbers. It
// shares the cache pool machinery with runBenchmark (one side instead of two).
func runSingleEnv(ctx context.Context, probes []dsl.Probe, env Env, samples, concurrency int, run runFunc, store *cache.Store, noCache bool, j judge.Judge) ([]aggregator.AggregatedOutcome, error) {
	recs := make([][]*parser.RunRecord, len(probes))
	pools, err := planPools(store, probes, samples, noCache, []side{{env, recs, "run"}})
	if err != nil {
		return nil, err
	}
	if err := runMisses(ctx, pools, concurrency, run, store, env); err != nil {
		return nil, err
	}
	if err := fillFromCache(store, pools, samples, noCache); err != nil {
		return nil, err
	}

	aggs := make([]aggregator.AggregatedOutcome, len(probes))
	gg, ggctx := errgroup.WithContext(ctx)
	gg.SetLimit(concurrency)
	var graded int64
	for pi, probe := range probes {
		gg.Go(func() error {
			agg, _, err := runner.GradeProbe(ggctx, probe, recs[pi], nil, j)
			if err != nil {
				return fmt.Errorf("probe %s: %w", probe.ID, err)
			}
			aggs[pi] = agg
			fmt.Fprintf(os.Stderr, "[graded %d/%d] %s\n", atomic.AddInt64(&graded, 1), len(probes), probe.ID)
			return nil
		})
	}
	if err := gg.Wait(); err != nil {
		return nil, err
	}
	return aggs, nil
}

func printAssessPlan(w io.Writer, envDesc, corpus string, probes []dsl.Probe, samples, concurrency, cached, toRun int) {
	var rules, comparative int
	for _, p := range probes {
		if p.Comparative() {
			comparative++
		} else {
			rules++
		}
	}
	fmt.Fprintln(w, "Assessment plan (single environment)")
	fmt.Fprintf(w, "  Environment: %s\n", envDesc)
	fmt.Fprintf(w, "  Corpus:      %s (%d probe(s): %d rule-based, %d comparative)\n", corpus, len(probes), rules, comparative)
	fmt.Fprintf(w, "  Samples:     %d\n", samples)
	fmt.Fprintf(w, "  Concurrency: %d\n", concurrency)
	fmt.Fprintf(w, "  Runs:        %d cached · %d to run (of %d: %d probes × %d samples)\n", cached, toRun, len(probes)*samples, len(probes), samples)
}

func init() {
	f := assessCmd.Flags()
	f.BoolVar(&flagAssessJudge, "judge", false, "build the Judge for judge_rubric rules (adds claude -p calls — extra spend)")
	f.StringVar(&flagAssessKind, "kind", allKinds, "probe kinds to run (CSV of rule_based,open_ended,plan)")
	f.StringVar(&flagAssessTarget, "target", ".", "path to the target repo under test")
	f.StringVar(&flagAssessCorpus, "corpus", "", "corpus YAML (default: auto-discover .benchmark/corpus/*.yaml)")
	f.StringVar(&flagAssessRef, "ref", "", "the Environment to assess (default: the working tree)")
	f.StringVar(&flagAssessMCP, "mcp", "", "MCP config for the assessed Environment (a --mcp-config file path or inline JSON)")
	f.IntVar(&flagAssessSamples, "samples", 1, "samples per probe (odd N; default 1)")
	f.IntVar(&flagAssessConcurrency, "concurrency", 8, "max runs in flight (>=1; Duration is advisory above 1)")
	f.BoolVar(&flagAssessYes, "yes", false, "skip the spend-plan confirmation prompt")
	addCacheFlags(f, &assessCacheFlags)
	rootCmd.AddCommand(assessCmd)
}
