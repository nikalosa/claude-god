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
	"github.com/nikalosa/claude-god/internal/cache"
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
	runCacheFlags     cacheFlags
)

// Env is one side of a comparison: the git ref under test plus the MCP config it
// carries. It makes the CONTEXT.md "Environment" concrete — the ref is the base
// layer (code + in-tree CLAUDE.md/rules/docs), MCPConfig an explicit layer on
// top — so Before and After can share a ref and differ only in MCP.
//
// Volatile marks the uncommitted working-tree snapshot (a synthetic, throwaway
// commit). The Run cache is baseline-only: a volatile side is never read or
// written, because it changes every iteration so a hit is impossible and a write
// would only mint a junk fingerprint dir per run (ADR-0016). Committed refs cache
// normally.
type Env struct {
	Ref       string
	MCPConfig string
	Volatile  bool
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
		mem := memPolicy{noSnapshot: flagNoMemSnapshot}
		store, err := newStore(target, mem, runCacheFlags, flagConcurrency)
		if err != nil {
			return err
		}
		run, cleanup, err := sharedRun(ctx, target, mem, runCacheFlags.model, runCacheFlags.effort, before, after)
		if err != nil {
			return err
		}
		defer cleanup()
		verdicts, prefs, aggs, err := runBenchmark(ctx, probes, before, after, flagSamples, flagConcurrency, run, store, runCacheFlags.noCache, j, flagDumpDir)
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

// runBenchmark serves every probe's Sample pool on the before and after sides
// from the Run cache, running only the misses in a bounded parallel pool, then
// grades the probes in a second bounded pool, returning the verdicts, open-ended
// preferences, and Numbers. Each completed run is written through to the cache
// immediately (crash-resume), and the pools are re-read from disk before grading
// so the graded order is exactly the cached order — making two runs of identical
// inputs produce identical grades, pool[0] stable for the Preference comparison
// (ADR-0016). --no-cache bypasses the read (fresh draws) but still writes.
// Concurrency is a scheduling detail: results are keyed by probe, so it never
// changes the output, only wall-clock Duration (ADR-0005). run is injected so
// the pool is testable without claude.
func runBenchmark(ctx context.Context, probes []dsl.Probe, before, after Env, samples, concurrency int, run runFunc, store *cache.Store, noCache bool, j judge.Judge, dumpDir string) ([]aggregator.Verdict, []runner.PreferenceResult, []aggregator.AggregatedOutcome, error) {
	beforeRecs := make([][]*parser.RunRecord, len(probes))
	afterRecs := make([][]*parser.RunRecord, len(probes))

	pools, err := planPools(store, probes, samples, noCache, []side{
		{before, beforeRecs, "before"}, {after, afterRecs, "after"},
	})
	if err != nil {
		return nil, nil, nil, err
	}

	if err := runMisses(ctx, pools, concurrency, run, store, before, after); err != nil {
		return nil, nil, nil, err
	}
	if err := fillFromCache(store, pools, samples, noCache); err != nil {
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

// side is one comparison arm fed to planPools: the Environment, the per-probe
// record slices to fill, and the label half.
type side struct {
	env  Env
	recs [][]*parser.RunRecord
	name string
}

// pool is one (probe, side) Sample pool: its cache key, how many fresh runs
// (misses) it needs, the fresh records those runs produced (slot-indexed, so
// concurrent writers need no lock), and where its graded records land.
type pool struct {
	env     Env
	prompt  string
	key     string
	label   string
	misses  int
	fresh   []*parser.RunRecord
	dst     *[]*parser.RunRecord
	compare bool
}

// planPools resolves each (probe, side) to its cache key, counts how many of the
// requested samples are already served from the cache, and sizes the misses to
// run. A volatile side (the uncommitted working tree) skips the cache entirely —
// no key, no read — so all its samples are misses (ADR-0016: baseline-only cache).
// Comparative pools sort first (LPT): open-ended and plan runs take ~5x longer, so
// dispatching their misses at t=0 flattens the tail.
func planPools(store *cache.Store, probes []dsl.Probe, samples int, noCache bool, sides []side) ([]pool, error) {
	var pools []pool
	for pi := range probes {
		prompt := taskPrompt(probes[pi])
		for _, sd := range sides {
			served := 0
			var key string
			if !sd.env.Volatile {
				k, err := store.Key(sd.env.Ref, sd.env.MCPConfig, prompt)
				if err != nil {
					return nil, fmt.Errorf("cache key for probe %s: %w", probes[pi].ID, err)
				}
				key = k
				if !noCache {
					existing, err := store.Read(key)
					if err != nil {
						return nil, fmt.Errorf("read cache for probe %s: %w", probes[pi].ID, err)
					}
					served = min(len(existing), samples)
				}
			}
			misses := samples - served
			pools = append(pools, pool{
				env:     sd.env,
				prompt:  prompt,
				key:     key,
				label:   fmt.Sprintf("probe %s %s", probes[pi].ID, sd.name),
				misses:  misses,
				fresh:   make([]*parser.RunRecord, misses),
				dst:     &sd.recs[pi],
				compare: probes[pi].Comparative(),
			})
		}
	}
	sort.SliceStable(pools, func(a, b int) bool { return pools[a].compare && !pools[b].compare })
	return pools, nil
}

// missTask is one fresh run: pool p, writing its result into p.fresh[slot].
type missTask struct {
	p    *pool
	slot int
}

// runMisses runs every pool's missing samples in one bounded pool and writes each
// completed run through to the cache immediately (crash-resume + the time win).
// A fully-cached invocation has no misses and never enters claude (so a lazy
// worktree is never prepared). Each result also lands in p.fresh[slot] so the
// --no-cache path can grade exactly the draws it just made.
func runMisses(ctx context.Context, pools []pool, concurrency int, run runFunc, store *cache.Store, guardEnvs ...Env) error {
	var tasks []missTask
	for i := range pools {
		for k := 0; k < pools[i].misses; k++ {
			tasks = append(tasks, missTask{&pools[i], k})
		}
	}
	if len(tasks) == 0 {
		return nil
	}

	start := time.Now()
	runLimit, retryGate := mcpGuard(concurrency, guardEnvs...)
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(runLimit)
	var done, inTok, outTok, costMicros int64
	total := int64(len(tasks))
	for _, t := range tasks {
		g.Go(func() error {
			rec, err := runWithRetry(gctx, run, t.p.env, t.p.prompt, t.p.label, retryGate)
			if err != nil {
				return fmt.Errorf("%s: %w", t.p.label, err)
			}
			if !t.p.env.Volatile {
				if err := store.Append(t.p.key, rec); err != nil {
					return fmt.Errorf("%s: cache write: %w", t.p.label, err)
				}
			}
			t.p.fresh[t.slot] = rec
			atomic.AddInt64(&inTok, int64(rec.TotalInputTokens()))
			atomic.AddInt64(&outTok, int64(rec.TotalOutputTokens()))
			atomic.AddInt64(&costMicros, int64(rec.TotalCost*1e6))
			fmt.Fprintf(os.Stderr, "[%d/%d run] %s · %s in / %s out · $%.2f · %s\n",
				atomic.AddInt64(&done, 1), total, t.p.label,
				humanCount(atomic.LoadInt64(&inTok)), humanCount(atomic.LoadInt64(&outTok)),
				float64(atomic.LoadInt64(&costMicros))/1e6,
				time.Since(start).Round(time.Second))
			return nil
		})
	}
	return g.Wait()
}

// fillFromCache picks the N records each pool grades. A cached side re-reads the
// pool from disk and takes the deterministic prefix, so the graded order is
// exactly the persisted order — a re-run of identical inputs grades the identical
// pool, pool[0] stable for the Preference comparison. A volatile side (uncommitted
// working tree, never persisted) and --no-cache both grade the fresh draws just
// made (p.fresh); the latter also keeps the two arms of a Before-vs-Before
// calibration independent even though they share one Fingerprint.
func fillFromCache(store *cache.Store, pools []pool, samples int, noCache bool) error {
	for i := range pools {
		if noCache || pools[i].env.Volatile {
			*pools[i].dst = pools[i].fresh
			continue
		}
		full, err := store.Read(pools[i].key)
		if err != nil {
			return fmt.Errorf("re-read cache for %s: %w", pools[i].label, err)
		}
		if len(full) < samples {
			return fmt.Errorf("cache pool for %s has %d records, need %d", pools[i].label, len(full), samples)
		}
		*pools[i].dst = full[:samples]
	}
	return nil
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

// sharedRun returns a runFunc that prepares one worktree per distinct ref
// lazily — on the first miss that needs that ref — and dispatches each Env to its
// ref's shared worktree, plus a cleanup that tears down whatever was prepared
// (ADR-0015). Lazy preparation is what makes a fully-cached ref do zero checkouts
// (ADR-0016 §9): the cache lookup runs first, and if every sample is served the
// runFunc is never called, so Prepare never fires. Memory is injected once per
// ref and removed in cleanup; the caller defers cleanup so it fires only after
// the run pool drains — a per-run teardown would wipe the slug sibling runs are
// still writing. Same-ref Before/After collapse to one worktree and differ only
// in the per-run MCP config; model/effort are the controlled run variables.
func sharedRun(ctx context.Context, target string, mem memPolicy, model, effort string, envs ...Env) (runFunc, func(), error) {
	type lazyTree struct {
		once sync.Once
		wt   *harness.Worktree
		err  error
	}
	trees := map[string]*lazyTree{}
	for _, ref := range distinctRefs(envs) {
		trees[ref] = &lazyTree{}
	}
	cleanup := func() {
		for _, lt := range trees {
			if lt.wt != nil {
				_ = lt.wt.Close()
			}
		}
	}
	prepare := func(ref string) (*harness.Worktree, error) {
		lt, ok := trees[ref]
		if !ok {
			return nil, fmt.Errorf("no tracked worktree for ref %q", ref)
		}
		lt.once.Do(func() {
			lt.wt, lt.err = harness.Prepare(ctx, harness.PrepareOpts{
				TargetRepo:    target,
				Ref:           ref,
				NoMemSnapshot: mem.noSnapshot,
				MemorySource:  mem.source,
				Model:         model,
				Effort:        effort,
			})
		})
		return lt.wt, lt.err
	}
	run := func(ctx context.Context, env Env, prompt string) (*parser.RunRecord, error) {
		wt, err := prepare(env.Ref)
		if err != nil {
			return nil, fmt.Errorf("prepare worktree for %s: %w", env.Ref, err)
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
	addCacheFlags(f, &runCacheFlags)
}
