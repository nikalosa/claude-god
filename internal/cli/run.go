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

type Env struct {
	Ref       string
	MCPConfig string
	Volatile  bool
}

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

func validateSamples(n int) error {
	if n < 1 {
		return fmt.Errorf("--samples must be >= 1")
	}
	if n%2 == 0 {
		return fmt.Errorf("--samples must be odd (got %d) so median == majority vote", n)
	}
	return nil
}

func judgeFor(probes []dsl.Probe, judgeOn bool) (judge.Judge, error) {
	if !dsl.NeedsJudge(probes) {
		return nil, nil
	}
	if !judgeOn {
		return nil, fmt.Errorf("corpus needs a judge (open-ended/plan/judge_rubric probes); pass --judge to enable it (adds claude -p calls — extra spend)")
	}
	return judge.New(judge.NewClaudeBackend()), nil
}

func taskPrompt(probe dsl.Probe) string {
	if probe.Kind == dsl.Plan {
		return "Produce a step-by-step plan to accomplish the following. Do not execute it.\n\n" + probe.Prompt
	}
	return probe.Prompt
}

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

type side struct {
	env  Env
	recs [][]*parser.RunRecord
	name string
}

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

type missTask struct {
	p    *pool
	slot int
}

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

func runAttempt(ctx context.Context, run runFunc, env Env, prompt string, attempt int, gate *sync.Mutex) (*parser.RunRecord, error) {
	if attempt > 1 && gate != nil {
		gate.Lock()
		defer gate.Unlock()
	}
	return run(ctx, env, prompt)
}

const mcpRunConcurrencyCap = 8

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

type memPolicy struct {
	noSnapshot bool
	source     string
}

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
