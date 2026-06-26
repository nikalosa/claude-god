package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync/atomic"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"

	"github.com/nikalosa/claude-god/internal/aggregator"
	"github.com/nikalosa/claude-god/internal/autodetect"
	"github.com/nikalosa/claude-god/internal/dsl"
	"github.com/nikalosa/claude-god/internal/harness"
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
	ref, desc, err := autodetect.ResolveOne(ctx, target, flagAssessRef)
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

	printAssessPlan(os.Stderr, desc, corpusPath, probes, flagAssessSamples, flagAssessConcurrency)
	ok, err := confirm(flagAssessYes, os.Stdin)
	if err != nil {
		return err
	}
	if !ok {
		fmt.Fprintln(os.Stderr, "aborted.")
		return nil
	}

	memSrc, err := memorySourceFor(target)
	if err != nil {
		return err
	}
	run := func(ctx context.Context, env Env, prompt string) (*parser.RunRecord, error) {
		r, err := harness.Run(ctx, harness.Opts{
			TargetRepo:   target,
			Branch:       env.Ref,
			Prompt:       prompt,
			MemorySource: memSrc,
			MCPConfig:    env.MCPConfig,
		})
		if err != nil {
			return nil, err
		}
		return r.Record, nil
	}

	aggs, err := runSingleEnv(ctx, probes, Env{Ref: ref, MCPConfig: flagAssessMCP}, flagAssessSamples, flagAssessConcurrency, run, j)
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

// runSingleEnv samples every probe on one ref in a bounded pool, then grades each
// probe with an empty After side: rule_based probes grade absolutely, comparative
// probes skip the Preference comparison (GradeProbe needs two answers) and keep
// only their Numbers. Like runBenchmark, results store by probe index so
// concurrency never changes the output (ADR-0005); comparative probes dispatch
// first (LPT) since they run ~5x longer.
func runSingleEnv(ctx context.Context, probes []dsl.Probe, env Env, samples, concurrency int, run runFunc, j judge.Judge) ([]aggregator.AggregatedOutcome, error) {
	recs := make([][]*parser.RunRecord, len(probes))
	for pi := range probes {
		recs[pi] = make([]*parser.RunRecord, samples)
	}

	order := make([]int, len(probes))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool {
		return probes[order[a]].Comparative() && !probes[order[b]].Comparative()
	})

	type task struct {
		prompt, label string
		dst           **parser.RunRecord
	}
	var tasks []task
	for _, pi := range order {
		probe := probes[pi]
		prompt := taskPrompt(probe)
		for si := 0; si < samples; si++ {
			tasks = append(tasks, task{prompt, fmt.Sprintf("probe %s sample %d", probe.ID, si+1), &recs[pi][si]})
		}
	}

	start := time.Now()
	runLimit, retryGate := mcpGuard(concurrency, env)
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(runLimit)
	var done, inTok, outTok, costMicros int64
	total := len(tasks)
	for _, t := range tasks {
		g.Go(func() error {
			rec, err := runWithRetry(gctx, run, env, t.prompt, t.label, retryGate)
			if err != nil {
				return fmt.Errorf("%s: %w", t.label, err)
			}
			*t.dst = rec
			atomic.AddInt64(&inTok, int64(rec.TotalInputTokens()))
			atomic.AddInt64(&outTok, int64(rec.TotalOutputTokens()))
			atomic.AddInt64(&costMicros, int64(rec.TotalCost*1e6))
			fmt.Fprintf(os.Stderr, "[%d/%d] %s in / %s out · $%.2f · %s\n",
				atomic.AddInt64(&done, 1), total,
				humanCount(atomic.LoadInt64(&inTok)), humanCount(atomic.LoadInt64(&outTok)),
				float64(atomic.LoadInt64(&costMicros))/1e6,
				time.Since(start).Round(time.Second))
			return nil
		})
	}
	if err := g.Wait(); err != nil {
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

func printAssessPlan(w io.Writer, envDesc, corpus string, probes []dsl.Probe, samples, concurrency int) {
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
	fmt.Fprintf(w, "  Runs:        %d claude -p calls (%d probes × %d samples)\n", len(probes)*samples, len(probes), samples)
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
	rootCmd.AddCommand(assessCmd)
}
