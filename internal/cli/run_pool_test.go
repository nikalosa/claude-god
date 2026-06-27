package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nikalosa/claude-god/internal/aggregator"
	"github.com/nikalosa/claude-god/internal/cache"
	"github.com/nikalosa/claude-god/internal/dsl"
	"github.com/nikalosa/claude-god/internal/judge"
	"github.com/nikalosa/claude-god/internal/parser"
	"github.com/nikalosa/claude-god/internal/report"
)

// tc is a Run cache backed by a fresh temp dir with a faked resolver (no git),
// so the pool tests exercise the real cache path without shelling out. A fresh
// store per call keeps determinism tests honest: both concurrency levels run all
// samples rather than the second serving the first's writes.
func tc(t *testing.T) *cache.Store {
	t.Helper()
	return cache.New(cache.Opts{
		Root:            t.TempDir(),
		Model:           "m",
		Effort:          "e",
		CLIVersionKey:   "v",
		CLIVersionStamp: "v",
		MemTag:          "none",
		Concurrency:     1,
		Resolve:         func(ref, mcp string) (string, string, error) { return ref, mcp, nil },
	})
}

// fakeRun is a deterministic runFunc: its record is a pure function of
// (env, prompt), so the only thing that can vary between concurrency levels
// is the pool's own scheduling — which must not change the result.
func fakeRun(_ context.Context, env Env, prompt string) (*parser.RunRecord, error) {
	pass := (prompt == "A" && env.Ref == "before") || (prompt == "B" && env.Ref == "after")
	text := "nope"
	if pass {
		text = "PASS"
	}
	return &parser.RunRecord{
		FinalText:  text,
		TotalCost:  0.01,
		Timing:     parser.Timing{DurationMs: 100},
		Usage:      parser.Usage{InputTokens: 10, OutputTokens: 5},
		ModelUsage: map[string]parser.ModelUsage{"m": {InputTokens: 10, OutputTokens: 5, CostUSD: 0.01}},
	}, nil
}

func poolTestProbes() []dsl.Probe {
	mk := func(id, prompt string) dsl.Probe {
		return dsl.Probe{ID: id, Prompt: prompt, Kind: dsl.RuleBased, Rules: []dsl.Rule{{
			ID: "r", Severity: dsl.Critical, Checks: []dsl.Check{&dsl.TextMatches{Pattern: regexp.MustCompile("PASS")}},
		}}}
	}
	return []dsl.Probe{mk("A", "A"), mk("B", "B")}
}

// TestRunBenchmark_DeterministicAcrossConcurrency is the core guarantee: the
// pool is a scheduling detail, so verdicts and Numbers must be identical at
// --concurrency 1 and 8 for the same inputs. Run with -race for the indexed
// writes.
func TestRunBenchmark_DeterministicAcrossConcurrency(t *testing.T) {
	probes := poolTestProbes()
	ctx := context.Background()

	v1, p1, a1, err := runBenchmark(ctx, probes, Env{Ref: "before"}, Env{Ref: "after"}, 3, 1, fakeRun, tc(t), false, nil, "")
	if err != nil {
		t.Fatalf("concurrency 1: %v", err)
	}
	v8, p8, a8, err := runBenchmark(ctx, probes, Env{Ref: "before"}, Env{Ref: "after"}, 3, 8, fakeRun, tc(t), false, nil, "")
	if err != nil {
		t.Fatalf("concurrency 8: %v", err)
	}

	if !reflect.DeepEqual(v1, v8) {
		t.Errorf("verdicts differ by concurrency:\n c1=%+v\n c8=%+v", v1, v8)
	}
	if !reflect.DeepEqual(a1, a8) {
		t.Errorf("aggregates differ by concurrency:\n c1=%+v\n c8=%+v", a1, a8)
	}
	if !reflect.DeepEqual(p1, p8) {
		t.Errorf("preferences differ by concurrency")
	}

	// Guard against a vacuous pass: the fixture must actually produce a flip in
	// each direction, so the determinism check is grading something real.
	var reg, newp int
	for _, v := range v1 {
		switch v.Status {
		case aggregator.Regression:
			reg++
		case aggregator.NewPass:
			newp++
		}
	}
	if reg == 0 || newp == 0 {
		t.Fatalf("fixture is vacuous: want a regression and a new pass, got reg=%d newp=%d (%+v)", reg, newp, v1)
	}
}

// TestRunBenchmark_DumpDirWritesAnswers proves the dumpDir seam threads through:
// when set, runBenchmark writes one Markdown file per probe holding the judged
// Before/After answers, and an index. An empty dumpDir (every other test) writes
// nothing.
func TestRunBenchmark_DumpDirWritesAnswers(t *testing.T) {
	dir := t.TempDir()
	probes := openEndedProbes("alpha", "beta")
	j := judge.StubJudge{Pref: judge.Preference{Outcome: judge.AfterBetter}}

	if _, _, _, err := runBenchmark(context.Background(), probes, Env{Ref: "before"}, Env{Ref: "after"}, 1, 4, fakeRun, tc(t), false, j, dir); err != nil {
		t.Fatalf("runBenchmark: %v", err)
	}

	for _, name := range []string{"index.md", "01-alpha.md", "02-beta.md"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("expected dump file %s: %v", name, err)
		}
	}
	doc, err := os.ReadFile(filepath.Join(dir, "01-alpha.md"))
	if err != nil {
		t.Fatalf("read dump: %v", err)
	}
	if !strings.Contains(string(doc), "## Before") || !strings.Contains(string(doc), "## After") {
		t.Errorf("dump missing Before/After sections:\n%s", doc)
	}
}

// TestMCPGuard pins the MCP-aware concurrency policy: with no Environment
// declaring MCP the pool runs at the requested concurrency and retries are
// unsynchronized (gate nil); as soon as one declares MCP the first-pass
// concurrency is capped and a retry gate is returned so re-rolls serialize. The
// cap never raises a concurrency already below it.
func TestMCPGuard(t *testing.T) {
	if limit, gate := mcpGuard(8, Env{Ref: "before"}, Env{Ref: "after"}); limit != 8 || gate != nil {
		t.Errorf("no MCP: want (8, nil), got (%d, %v)", limit, gate)
	}
	if limit, gate := mcpGuard(8, Env{Ref: "before"}, Env{Ref: "after", MCPConfig: "/tmp/x.json"}); limit != mcpRunConcurrencyCap || gate == nil {
		t.Errorf("MCP declared: want (%d, non-nil), got (%d, %v)", mcpRunConcurrencyCap, limit, gate)
	}
	if limit, _ := mcpGuard(2, Env{MCPConfig: "x"}); limit != 2 {
		t.Errorf("cap must not raise concurrency below it, want 2 got %d", limit)
	}
}

func openEndedProbes(ids ...string) []dsl.Probe {
	probes := make([]dsl.Probe, len(ids))
	for i, id := range ids {
		probes[i] = dsl.Probe{ID: id, Prompt: id, Kind: dsl.OpenEnded}
	}
	return probes
}

// TestRunBenchmark_JudgeDeterministicAcrossConcurrency extends the determinism
// guarantee to the judge path: open-ended probes route through j.Prefer, and
// the compacted preferences must stay in probe order regardless of which task
// finishes first. Run with -race for the indexed writes.
func TestRunBenchmark_JudgeDeterministicAcrossConcurrency(t *testing.T) {
	probes := openEndedProbes("alpha", "beta", "gamma")
	ctx := context.Background()
	j := judge.StubJudge{Pref: judge.Preference{Outcome: judge.AfterBetter}}

	v1, p1, a1, err := runBenchmark(ctx, probes, Env{Ref: "before"}, Env{Ref: "after"}, 3, 1, fakeRun, tc(t), false, j, "")
	if err != nil {
		t.Fatalf("concurrency 1: %v", err)
	}
	v8, p8, a8, err := runBenchmark(ctx, probes, Env{Ref: "before"}, Env{Ref: "after"}, 3, 8, fakeRun, tc(t), false, j, "")
	if err != nil {
		t.Fatalf("concurrency 8: %v", err)
	}

	if !reflect.DeepEqual(v1, v8) || !reflect.DeepEqual(a1, a8) || !reflect.DeepEqual(p1, p8) {
		t.Errorf("results differ by concurrency:\n c1 v=%+v p=%+v\n c8 v=%+v p=%+v", v1, p1, v8, p8)
	}
	if len(p1) != len(probes) {
		t.Fatalf("want a preference per open-ended probe, got %d/%d", len(p1), len(probes))
	}
	for i, pr := range p1 {
		if pr.ProbeID != probes[i].ID {
			t.Errorf("preference %d out of probe order: got %q want %q", i, pr.ProbeID, probes[i].ID)
		}
	}
}

// TestRunBenchmark_GradesConcurrently proves grading actually overlaps rather
// than running serially: a judge that blocks until every probe's Prefer call
// has entered can only return if all of them run at once. If grading were
// serial, the barrier never reaches N and the test fails on timeout.
func TestRunBenchmark_GradesConcurrently(t *testing.T) {
	const n = 4
	probes := openEndedProbes("p1", "p2", "p3", "p4")

	var arrived int64
	allIn := make(chan struct{})
	release := make(chan struct{})
	j := judge.StubJudge{PreferFunc: func(ctx context.Context, _, _, _ string) (judge.Preference, error) {
		if atomic.AddInt64(&arrived, 1) == n {
			close(allIn)
		}
		<-release
		return judge.Preference{Outcome: judge.Tie}, nil
	}}

	done := make(chan error, 1)
	go func() {
		_, _, _, err := runBenchmark(context.Background(), probes, Env{Ref: "before"}, Env{Ref: "after"}, 1, n, fakeRun, tc(t), false, j, "")
		done <- err
	}()

	select {
	case <-allIn:
		close(release)
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatalf("only %d/%d grading tasks ran concurrently — grading is serial", atomic.LoadInt64(&arrived), n)
	}
	if err := <-done; err != nil {
		t.Fatalf("runBenchmark: %v", err)
	}
}

// TestRunBenchmark_HardGradeErrorAborts: a judge_rubric Score failure is a hard
// grade error, so the whole phase aborts (errgroup first-error) rather than
// reporting a half-graded matrix.
func TestRunBenchmark_HardGradeErrorAborts(t *testing.T) {
	probes := []dsl.Probe{{ID: "j", Prompt: "q", Kind: dsl.RuleBased, Rules: []dsl.Rule{{
		ID: "r", Severity: dsl.Critical, Checks: []dsl.Check{&dsl.JudgeRubric{Facts: []string{"f"}, PassScore: 50}},
	}}}}
	j := judge.StubJudge{ScoreErr: errors.New("boom")}

	if _, _, _, err := runBenchmark(context.Background(), probes, Env{Ref: "before"}, Env{Ref: "after"}, 1, 4, fakeRun, tc(t), false, j, ""); err == nil {
		t.Fatal("want runBenchmark to abort on a hard grade error, got nil")
	}
}

// TestRunBenchmark_PreferenceErrorIsDropped: a preference is report-only, so a
// failing Prefer must not abort — the probe keeps its Numbers and every other
// probe survives, that probe just contributes no preference.
func TestRunBenchmark_PreferenceErrorIsDropped(t *testing.T) {
	probes := openEndedProbes("alpha", "beta")
	j := judge.StubJudge{PrefErr: errors.New("boom")}

	verdicts, prefs, aggs, err := runBenchmark(context.Background(), probes, Env{Ref: "before"}, Env{Ref: "after"}, 1, 4, fakeRun, tc(t), false, j, "")
	if err != nil {
		t.Fatalf("preference error must not abort: %v", err)
	}
	if len(prefs) != 0 {
		t.Errorf("want no preferences when every Prefer fails, got %d", len(prefs))
	}
	if len(aggs) != len(probes) {
		t.Errorf("want Numbers kept for every probe, got %d/%d", len(aggs), len(probes))
	}
	_ = verdicts
}

// TestRunBenchmark_PlanProbeEndToEnd walks the whole plan path with a stub
// judge: a kind: plan probe routes through the comparative path, produces a
// report-only preference (no rule verdicts), and renders in the report.
func TestRunBenchmark_PlanProbeEndToEnd(t *testing.T) {
	probe := dsl.Probe{ID: "rollout", Prompt: "Plan the migration.", Kind: dsl.Plan}
	j := judge.StubJudge{Pref: judge.Preference{
		Outcome: judge.AfterBetter, Concise: judge.AfterBetter,
		Exhaustive: judge.Tie, Direct: judge.AfterBetter, Reasoning: "after has clearer steps",
	}}

	verdicts, prefs, deltas, err := runBenchmark(context.Background(), []dsl.Probe{probe}, Env{Ref: "before"}, Env{Ref: "after"}, 1, 1, fakeRun, tc(t), false, j, "")
	if err != nil {
		t.Fatalf("runBenchmark: %v", err)
	}
	if len(verdicts) != 0 {
		t.Errorf("plan probe must produce no rule verdicts, got %d", len(verdicts))
	}
	if len(prefs) != 1 || prefs[0].ProbeID != "rollout" || prefs[0].Outcome != judge.AfterBetter {
		t.Fatalf("expected one plan preference (after better), got %+v", prefs)
	}

	md := report.RenderMarkdown(verdicts, prefs, deltas, 1)
	if !strings.Contains(md, "What reads better") || !strings.Contains(md, "rollout") {
		t.Errorf("report should render the plan preference, got:\n%s", md)
	}
}
