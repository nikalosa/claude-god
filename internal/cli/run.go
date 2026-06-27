package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
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
	flagJudge         bool
	flagKind          string
	flagTarget        string
	flagCorpus        string
	flagBefore        string
	flagAfter         string
	flagBeforeMCP     string
	flagAfterMCP      string
	flagSamples       int
	flagConcurrency   int
	flagNoMemSnapshot bool
	flagDumpDir       string
)

// Env is one side of a comparison: the git ref under test plus the MCP config it
// carries. It makes the CONTEXT.md "Environment" concrete — the ref is the base
// layer (code + in-tree CLAUDE.md/rules/docs), MCPConfig an explicit layer on
// top — so Before and After can share a ref and differ only in MCP.
type Env struct {
	Ref       string
	MCPConfig string
}

// runFunc executes one probe sample in an Environment and returns its record.
// The pool depends on this seam, not on harness.Run, so it is unit-testable with
// a fake and never shells out to claude in tests.
type runFunc func(ctx context.Context, env Env, prompt string) (*parser.RunRecord, error)

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Run the A/B benchmark across Before and After",
	RunE: func(cmd *cobra.Command, args []string) error {
		kinds, err := parseKinds(flagKind)
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
		probes, err = filterByKind(probes, kinds)
		if err != nil {
			return err
		}

		j, err := judgeFor(probes, flagJudge)
		if err != nil {
			return err
		}

		ctx := context.Background()
		before := Env{Ref: flagBefore, MCPConfig: flagBeforeMCP}
		after := Env{Ref: flagAfter, MCPConfig: flagAfterMCP}
		run, cleanup, err := sharedRun(ctx, target, memPolicy{noSnapshot: flagNoMemSnapshot}, before, after)
		if err != nil {
			return err
		}
		defer cleanup()
		verdicts, prefs, aggs, err := runBenchmark(ctx, probes, before, after, flagSamples, flagConcurrency, run, j, flagDumpDir)
		if err != nil {
			return err
		}
		fmt.Println(report.RenderMarkdown(verdicts, prefs, aggs, flagConcurrency))
		return nil
	},
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

// judgeFor builds a Judge iff the corpus needs one (judge-backed rules or
// comparative probes — open-ended and plan). --judge gates it: the Judge adds
// claude -p calls, so a corpus that needs one errors until --judge is passed.
func judgeFor(probes []dsl.Probe, judgeOn bool) (judge.Judge, error) {
	if !dsl.NeedsJudge(probes) {
		return nil, nil
	}
	if !judgeOn {
		return nil, fmt.Errorf("corpus needs a judge (open-ended/plan/judge_rubric probes); pass --judge to enable it (adds claude -p calls — extra spend)")
	}
	return judge.New(judge.NewClaudeBackend()), nil
}

// taskPrompt wraps a plan probe so the run is asked for a step-by-step plan
// (Mode = Plan). Runs are already read-only (ADR-0006), so this only makes the
// plan-not-execute intent explicit; other kinds pass through unchanged. The
// wrap is applied to the run prompt only — the judge still compares against the
// probe's original question.
func taskPrompt(probe dsl.Probe) string {
	if probe.Kind == dsl.Plan {
		return "Produce a step-by-step plan to accomplish the following. Do not execute it.\n\n" + probe.Prompt
	}
	return probe.Prompt
}

// runBenchmark samples every probe on the before and after branches in a
// bounded parallel pool, then grades the probes in a second bounded pool,
// returning the verdicts, open-ended preferences, and Numbers. Both pools are
// scheduling details: results are collected by index, so concurrency changes
// nothing about the result; only Duration inflates under concurrency (see
// ADR-0005). Shared by run and calibrate (calibrate passes the same branch on
// both sides). run is injected so the pool is testable without claude.
func runBenchmark(ctx context.Context, probes []dsl.Probe, before, after Env, samples, concurrency int, run runFunc, j judge.Judge, dumpDir string) ([]aggregator.Verdict, []runner.PreferenceResult, []aggregator.AggregatedOutcome, error) {
	beforeRecs := make([][]*parser.RunRecord, len(probes))
	afterRecs := make([][]*parser.RunRecord, len(probes))

	type task struct {
		spec               Env
		prompt, label, env string
		dst                **parser.RunRecord
	}
	for pi := range probes {
		beforeRecs[pi] = make([]*parser.RunRecord, samples)
		afterRecs[pi] = make([]*parser.RunRecord, samples)
	}

	// Dispatch comparative probes first (LPT): open-ended and plan probes run
	// ~5x longer than rule-based, so starting the long poles at t=0 and
	// backfilling with cheap probes flattens the tail. Results store by original
	// probe index, so dispatch order never changes the graded output.
	order := make([]int, len(probes))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool {
		return probes[order[a]].Comparative() && !probes[order[b]].Comparative()
	})

	var tasks []task
	for _, pi := range order {
		probe := probes[pi]
		prompt := taskPrompt(probe)
		for si := 0; si < samples; si++ {
			tasks = append(tasks,
				task{before, prompt, fmt.Sprintf("probe %s before sample %d", probe.ID, si+1), "before", &beforeRecs[pi][si]},
				task{after, prompt, fmt.Sprintf("probe %s after sample %d", probe.ID, si+1), "after", &afterRecs[pi][si]},
			)
		}
	}

	perEnv := int64(samples * len(probes))
	start := time.Now()
	runLimit, retryGate := mcpGuard(concurrency, before, after)
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(runLimit)
	var done, beforeDone, afterDone, inTok, outTok, costMicros int64
	total := len(tasks)
	for _, t := range tasks {
		g.Go(func() error {
			rec, err := runWithRetry(gctx, run, t.spec, t.prompt, t.label, retryGate)
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

	if dumpDir != "" {
		if err := report.DumpAnswers(dumpDir, before.Ref, after.Ref, probes, beforeRecs, afterRecs, prefSlots); err != nil {
			fmt.Fprintf(os.Stderr, "warning: answer dump failed: %v\n", err)
		} else {
			fmt.Fprintf(os.Stderr, "wrote answer dump to %s\n", dumpDir)
		}
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
// flake or API error, or a declared MCP server that lost the startup race —
// harness.checkMCPHealth surfaces that as an error): one dying run must not abort a
// whole 96-run benchmark. Each attempt is a fresh worktree + claude invocation.
// Stops early if the pool context is cancelled.
func runWithRetry(ctx context.Context, run runFunc, env Env, prompt, label string, gate *sync.Mutex) (*parser.RunRecord, error) {
	const maxAttempts = 3
	var err error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		var rec *parser.RunRecord
		if rec, err = runAttempt(ctx, run, env, prompt, attempt, gate); err == nil {
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

// runAttempt serializes re-rolls (attempt > 1) through gate when set. A declared
// MCP server only loses the startup race under CPU contention, so the first pass
// stays parallel while retries run one-at-a-time and reliably win the handshake.
// gate is nil when no Environment declares MCP, leaving every attempt unsynchronized.
func runAttempt(ctx context.Context, run runFunc, env Env, prompt string, attempt int, gate *sync.Mutex) (*parser.RunRecord, error) {
	if attempt > 1 && gate != nil {
		gate.Lock()
		defer gate.Unlock()
	}
	return run(ctx, env, prompt)
}

// mcpRunConcurrencyCap bounds the run pool when an Environment declares MCP. The
// headless client can start its turn before a stdio MCP server finishes its
// handshake; under enough CPU/IO contention the handshake loses and the turn runs
// with no MCP tools. It was 3 under the per-run-worktree regime, where every run
// also cold-started a heavy `git reset --hard` in the same window (measured then:
// 2 wins, 6 loses). Once ADR-0015 hoisted checkout to once-per-ref the startup
// window went quiet; re-measurement ran 12-way concurrent clean (26 runs, 0 misses),
// so the cap is raised to 8 as a backstop for pathological cases (huge index / slow
// disk / busy box). A miss is still detected (harness.checkMCPHealth) and serialized
// through the retry gate — correctness over speed, Duration advisory above 1.
const mcpRunConcurrencyCap = 8

// mcpGuard derives the run-pool limit and retry gate for a set of Environments.
// When any declares MCP it caps first-pass concurrency and returns a gate so
// retries serialize (see runAttempt); otherwise the pool runs at the requested
// concurrency with no gate. Note: only an explicit MCP config (--before-mcp /
// --after-mcp / --mcp) is visible here — a ref's committed .mcp.json is resolved
// later in the harness, so the cap does not engage for that case (ADR-0014).
func mcpGuard(concurrency int, envs ...Env) (limit int, gate *sync.Mutex) {
	limit = concurrency
	for _, e := range envs {
		if e.MCPConfig == "" {
			continue
		}
		if limit > mcpRunConcurrencyCap {
			limit = mcpRunConcurrencyCap
		}
		gate = &sync.Mutex{}
		fmt.Fprintf(os.Stderr, "MCP declared: capping run concurrency to %d and serializing retries so the stdio MCP handshake wins the startup race (correctness over speed; ADR-0014)\n", limit)
		return limit, gate
	}
	return limit, nil
}

// memPolicy is how a command injects project memory into each prepared
// worktree: an explicit live source (assess/bare), or the ref's committed
// snapshot unless --no-memory-snapshot is set (run).
type memPolicy struct {
	noSnapshot bool
	source     string
}

// sharedRun prepares one worktree per distinct ref (ADR-0015) and returns a
// runFunc that dispatches each Env to its ref's shared worktree, plus a cleanup
// that tears the worktrees down. Memory is injected once per ref in Prepare and
// removed in cleanup; the caller defers cleanup so it fires only after the run
// pool drains — a per-run teardown would wipe the slug sibling runs are still
// writing. Same-ref Before/After collapse to one worktree and differ only in the
// per-run MCP config.
func sharedRun(ctx context.Context, target string, mem memPolicy, envs ...Env) (runFunc, func(), error) {
	trees := map[string]*harness.Worktree{}
	cleanup := func() {
		for _, wt := range trees {
			_ = wt.Close()
		}
	}
	for _, ref := range distinctRefs(envs) {
		wt, err := harness.Prepare(ctx, harness.PrepareOpts{
			TargetRepo:    target,
			Ref:           ref,
			NoMemSnapshot: mem.noSnapshot,
			MemorySource:  mem.source,
		})
		if err != nil {
			cleanup()
			return nil, nil, fmt.Errorf("prepare worktree for %s: %w", ref, err)
		}
		trees[ref] = wt
	}
	run := func(ctx context.Context, env Env, prompt string) (*parser.RunRecord, error) {
		wt, ok := trees[env.Ref]
		if !ok {
			return nil, fmt.Errorf("no prepared worktree for ref %q", env.Ref)
		}
		res, err := wt.RunIn(ctx, prompt, env.MCPConfig)
		if err != nil {
			return nil, err
		}
		return res.Record, nil
	}
	return run, cleanup, nil
}

// distinctRefs returns the unique refs across envs, first-seen order, so a
// same-ref Before/After yields a single worktree.
func distinctRefs(envs []Env) []string {
	seen := map[string]bool{}
	var refs []string
	for _, e := range envs {
		if !seen[e.Ref] {
			seen[e.Ref] = true
			refs = append(refs, e.Ref)
		}
	}
	return refs
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
	f.BoolVar(&flagJudge, "judge", false, "build the Judge for open-ended/plan/judge_rubric corpora (adds claude -p calls — extra spend)")
	f.StringVar(&flagKind, "kind", allKinds, "probe kinds to run (CSV of rule_based,open_ended,plan)")
	f.StringVar(&flagTarget, "target", ".", "path to the target repo under test")
	f.StringVar(&flagCorpus, "corpus", "", "path to the probe corpus YAML file")
	f.StringVar(&flagBefore, "before", "benchmark/before", "branch holding the pre-restructure baseline")
	f.StringVar(&flagAfter, "after", "benchmark/after", "branch holding the post-restructure config under test")
	f.StringVar(&flagBeforeMCP, "before-mcp", "", "MCP config for Before (a --mcp-config file path or inline JSON; empty = the ref's committed .mcp.json, else none)")
	f.StringVar(&flagAfterMCP, "after-mcp", "", "MCP config for After (a --mcp-config file path or inline JSON; empty = the ref's committed .mcp.json, else none)")
	f.IntVar(&flagSamples, "samples", 1, "samples per environment (odd N; default 1, adaptive N=5 deferred)")
	f.IntVar(&flagConcurrency, "concurrency", 8, "max runs in flight (>=1; Duration is advisory above 1)")
	f.BoolVar(&flagNoMemSnapshot, "no-memory-snapshot", false, "skip pinning project memory into the run")
	f.StringVar(&flagDumpDir, "dump-dir", "", "write each probe's judged Before/After answers here (one Markdown file per probe)")
}
