package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"

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
	flagConcurrency   int
	flagNoMemSnapshot bool
)

// runFunc executes one probe sample on a branch and returns its record. The
// pool depends on this seam, not on harness.Run, so it is unit-testable with a
// fake and never shells out to claude in tests.
type runFunc func(ctx context.Context, branch, prompt string) (*parser.RunRecord, error)

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
		if err := validateConcurrency(flagConcurrency); err != nil {
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
		run := harnessRun(target, flagNoMemSnapshot)
		verdicts, prefs, aggs, err := runBenchmark(ctx, probes, flagBefore, flagAfter, flagSamples, flagConcurrency, run, j)
		if err != nil {
			return err
		}
		fmt.Println(report.RenderMarkdown(verdicts, prefs, aggs, flagConcurrency))
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

// runBenchmark samples every probe on the before and after branches in a
// bounded parallel pool, then grades the probes in a second bounded pool,
// returning the verdicts, open-ended preferences, and Numbers. Both pools are
// scheduling details: results are collected by index, so concurrency changes
// nothing about the result; only Duration inflates under concurrency (see
// ADR-0005). Shared by run and calibrate (calibrate passes the same branch on
// both sides). run is injected so the pool is testable without claude.
func runBenchmark(ctx context.Context, probes []dsl.Probe, before, after string, samples, concurrency int, run runFunc, j judge.Judge) ([]aggregator.Verdict, []runner.PreferenceResult, []aggregator.AggregatedOutcome, error) {
	beforeRecs := make([][]*parser.RunRecord, len(probes))
	afterRecs := make([][]*parser.RunRecord, len(probes))

	type task struct {
		branch, prompt, label, env string
		dst                        **parser.RunRecord
	}
	for pi := range probes {
		beforeRecs[pi] = make([]*parser.RunRecord, samples)
		afterRecs[pi] = make([]*parser.RunRecord, samples)
	}

	// Dispatch open-ended probes first (LPT): they run ~5x longer than
	// rule-based, so starting the long poles at t=0 and backfilling with cheap
	// probes flattens the tail. Results store by original probe index, so
	// dispatch order never changes the graded output.
	order := make([]int, len(probes))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool {
		return probes[order[a]].OpenEnded() && !probes[order[b]].OpenEnded()
	})

	var tasks []task
	for _, pi := range order {
		probe := probes[pi]
		for si := 0; si < samples; si++ {
			tasks = append(tasks,
				task{before, probe.Prompt, fmt.Sprintf("probe %s before sample %d", probe.ID, si+1), "before", &beforeRecs[pi][si]},
				task{after, probe.Prompt, fmt.Sprintf("probe %s after sample %d", probe.ID, si+1), "after", &afterRecs[pi][si]},
			)
		}
	}

	perEnv := int64(samples * len(probes))
	start := time.Now()
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(concurrency)
	var done, beforeDone, afterDone, inTok, outTok, costMicros int64
	total := len(tasks)
	for _, t := range tasks {
		g.Go(func() error {
			rec, err := runWithRetry(gctx, run, t.branch, t.prompt, t.label)
			if err != nil {
				return fmt.Errorf("%s: %w", t.label, err)
			}
			*t.dst = rec
			envDone := &afterDone
			if t.env == "before" {
				envDone = &beforeDone
			}
			atomic.AddInt64(envDone, 1)
			atomic.AddInt64(&inTok, int64(rec.TotalInputTokens()))
			atomic.AddInt64(&outTok, int64(rec.TotalOutputTokens()))
			atomic.AddInt64(&costMicros, int64(rec.TotalCost*1e6))
			fmt.Fprintf(os.Stderr, "[%d/%d] before %d/%d · after %d/%d · %s in / %s out · $%.2f · %s\n",
				atomic.AddInt64(&done, 1), total,
				atomic.LoadInt64(&beforeDone), perEnv,
				atomic.LoadInt64(&afterDone), perEnv,
				humanCount(atomic.LoadInt64(&inTok)), humanCount(atomic.LoadInt64(&outTok)),
				float64(atomic.LoadInt64(&costMicros))/1e6,
				time.Since(start).Round(time.Second))
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, nil, nil, err
	}

	// Grade the probes in the same bounded pool: only the judge calls (Prefer,
	// Score) shell claude -p, so concurrency overlaps that latency; aggregation
	// is pure CPU and rides along inside each task. Results are written by probe
	// index and preferences compacted in probe order after the pool drains, so
	// completion order never changes the report (same property as the run pool,
	// ADR-0005). First grade error cancels in-flight judge calls.
	aggs := make([]aggregator.AggregatedOutcome, len(probes))
	prefSlots := make([]*runner.PreferenceResult, len(probes))
	gg, ggctx := errgroup.WithContext(ctx)
	gg.SetLimit(concurrency)
	var graded int64
	for pi, probe := range probes {
		gg.Go(func() error {
			agg, pref, err := runner.GradeProbe(ggctx, probe, beforeRecs[pi], afterRecs[pi], j)
			if err != nil {
				return fmt.Errorf("probe %s: %w", probe.ID, err)
			}
			aggs[pi] = agg
			prefSlots[pi] = pref
			fmt.Fprintf(os.Stderr, "[graded %d/%d] %s\n", atomic.AddInt64(&graded, 1), len(probes), probe.ID)
			return nil
		})
	}
	if err := gg.Wait(); err != nil {
		return nil, nil, nil, err
	}

	var prefs []runner.PreferenceResult
	for _, p := range prefSlots {
		if p != nil {
			prefs = append(prefs, *p)
		}
	}
	return aggregator.Compare(aggs), prefs, aggs, nil
}

// runWithRetry retries a sample on transient failure (an occasional claude -p
// flake or API error): one dying run must not abort a whole 96-run benchmark.
// Each attempt is a fresh worktree + claude invocation. Stops early if the pool
// context is cancelled.
func runWithRetry(ctx context.Context, run runFunc, branch, prompt, label string) (*parser.RunRecord, error) {
	const maxAttempts = 3
	var err error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		var rec *parser.RunRecord
		if rec, err = run(ctx, branch, prompt); err == nil {
			return rec, nil
		}
		if ctx.Err() != nil {
			return nil, err
		}
		if attempt < maxAttempts {
			fmt.Fprintf(os.Stderr, "retry %d/%d · %s · %v\n", attempt, maxAttempts-1, label, err)
		}
	}
	return nil, err
}

// harnessRun is the production runFunc: it runs one sample through the real
// harness for a fixed target and memory policy. Each call gets its own worktree
// so concurrent claude sessions never share a cwd.
func harnessRun(target string, noMem bool) runFunc {
	return func(ctx context.Context, branch, prompt string) (*parser.RunRecord, error) {
		res, err := harness.Run(ctx, harness.Opts{
			TargetRepo:    target,
			Branch:        branch,
			Prompt:        prompt,
			NoMemSnapshot: noMem,
		})
		if err != nil {
			return nil, err
		}
		return res.Record, nil
	}
}

func humanCount(n int64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 1_000:
		return fmt.Sprintf("%.0fk", float64(n)/1e3)
	default:
		return fmt.Sprintf("%d", n)
	}
}

func validateConcurrency(n int) error {
	if n < 1 {
		return fmt.Errorf("--concurrency must be >= 1 (got %d)", n)
	}
	return nil
}

func init() {
	f := runCmd.Flags()
	f.StringVar(&flagLevel, "level", "l1", "comma-separated tiers to run (l1, l2)")
	f.StringVar(&flagTarget, "target", ".", "path to the target repo under test")
	f.StringVar(&flagCorpus, "corpus", "", "path to the probe corpus YAML file")
	f.StringVar(&flagBefore, "before", "validator/before", "branch holding the pre-restructure baseline")
	f.StringVar(&flagAfter, "after", "validator/after", "branch holding the post-restructure config under test")
	f.IntVar(&flagSamples, "samples", 3, "samples per environment (odd N; N=3 by default, adaptive N=5 deferred)")
	f.IntVar(&flagConcurrency, "concurrency", 8, "max runs in flight (>=1; Duration is advisory above 1)")
	f.BoolVar(&flagNoMemSnapshot, "no-memory-snapshot", false, "skip pinning project memory into the run")
}
