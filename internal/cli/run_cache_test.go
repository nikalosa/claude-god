package cli

import (
	"context"
	"fmt"
	"reflect"
	"sync/atomic"
	"testing"

	"github.com/nikalosa/claude-god/internal/dsl"
	"github.com/nikalosa/claude-god/internal/parser"
)

var before = Env{Ref: "before"}
var after = Env{Ref: "after"}

func countingFakeRun(calls *int64) runFunc {
	return func(ctx context.Context, env Env, prompt string) (*parser.RunRecord, error) {
		atomic.AddInt64(calls, 1)
		return fakeRun(ctx, env, prompt)
	}
}

func serialCountingRun(calls *int64) runFunc {
	return func(_ context.Context, _ Env, _ string) (*parser.RunRecord, error) {
		n := atomic.AddInt64(calls, 1)
		return &parser.RunRecord{
			FinalText:  fmt.Sprintf("rec%d", n),
			TotalCost:  0.01,
			ModelUsage: map[string]parser.ModelUsage{"m": {CostUSD: 0.01}},
		}, nil
	}
}

func TestCache_SecondRunAllHits(t *testing.T) {
	store := tc(t)
	probes := poolTestProbes()
	var calls int64
	run := countingFakeRun(&calls)

	v1, _, a1, err := runBenchmark(context.Background(), probes, before, after, 3, 4, run, store, false, nil, "")
	if err != nil {
		t.Fatalf("run 1: %v", err)
	}
	first := atomic.LoadInt64(&calls)
	if want := int64(2 * 3 * 2); first != want {
		t.Fatalf("run 1 should run every sample: want %d runs, got %d", want, first)
	}

	v2, _, a2, err := runBenchmark(context.Background(), probes, before, after, 3, 4, run, store, false, nil, "")
	if err != nil {
		t.Fatalf("run 2: %v", err)
	}
	if extra := atomic.LoadInt64(&calls) - first; extra != 0 {
		t.Errorf("run 2 must be all cache hits, but ran %d more samples", extra)
	}
	if !reflect.DeepEqual(v1, v2) || !reflect.DeepEqual(a1, a2) {
		t.Error("grades must be identical across a cache hit")
	}
}

func TestCache_ChangedPromptMisses(t *testing.T) {
	store := tc(t)
	var calls int64
	run := countingFakeRun(&calls)

	if _, _, _, err := runBenchmark(context.Background(), poolTestProbes(), before, after, 1, 4, run, store, false, nil, ""); err != nil {
		t.Fatal(err)
	}
	base := atomic.LoadInt64(&calls)

	edited := poolTestProbes()
	edited[0].Prompt = "A-edited"
	if _, _, _, err := runBenchmark(context.Background(), edited, before, after, 1, 4, run, store, false, nil, ""); err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt64(&calls) - base; got != 2 {
		t.Errorf("an edited prompt must miss on both sides (2 runs); unchanged probe must hit. got %d new runs", got)
	}
}

func TestCache_GrowShrinkPreservesPrefix(t *testing.T) {
	store := tc(t)
	probes := []dsl.Probe{poolTestProbes()[0]}
	keyB, _ := store.Key(before.Ref, "", taskPrompt(probes[0]))

	var grow int64
	if _, _, _, err := runBenchmark(context.Background(), probes, before, after, 1, 1, serialCountingRun(&grow), store, false, nil, ""); err != nil {
		t.Fatal(err)
	}
	p1, _ := store.Read(keyB)
	if len(p1) != 1 {
		t.Fatalf("N=1 should leave 1 record, got %d", len(p1))
	}
	head := p1[0].FinalText

	if _, _, _, err := runBenchmark(context.Background(), probes, before, after, 3, 1, serialCountingRun(&grow), store, false, nil, ""); err != nil {
		t.Fatal(err)
	}
	p3, _ := store.Read(keyB)
	if len(p3) != 3 {
		t.Fatalf("grow to N=3 should append, got %d", len(p3))
	}
	if p3[0].FinalText != head {
		t.Errorf("pool[0] must be stable across grow: %q -> %q", head, p3[0].FinalText)
	}

	var shrink int64
	if _, _, _, err := runBenchmark(context.Background(), probes, before, after, 1, 1, countingFakeRun(&shrink), store, false, nil, ""); err != nil {
		t.Fatal(err)
	}
	if shrink != 0 {
		t.Errorf("shrink to N=1 must run nothing, ran %d", shrink)
	}
	p3b, _ := store.Read(keyB)
	if len(p3b) != 3 || p3b[0].FinalText != head {
		t.Errorf("shrink must not discard or reorder the pool: len=%d head=%q", len(p3b), p3b[0].FinalText)
	}
}

func TestCache_ResumesFromCheckpoint(t *testing.T) {
	store := tc(t)
	probes := []dsl.Probe{poolTestProbes()[0]}
	keyB, _ := store.Key(before.Ref, "", taskPrompt(probes[0]))
	if err := store.Append(keyB, &parser.RunRecord{FinalText: "PASS", ModelUsage: map[string]parser.ModelUsage{"m": {}}}); err != nil {
		t.Fatal(err)
	}

	var calls int64
	if _, _, _, err := runBenchmark(context.Background(), probes, before, after, 1, 1, countingFakeRun(&calls), store, false, nil, ""); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Errorf("the before side is checkpointed, so only the after side should run; got %d runs", calls)
	}
}

func TestCache_NoCacheBypassesReadStillWrites(t *testing.T) {
	store := tc(t)
	probes := []dsl.Probe{poolTestProbes()[0]}
	keyB, _ := store.Key(before.Ref, "", taskPrompt(probes[0]))

	if _, _, _, err := runBenchmark(context.Background(), probes, before, after, 1, 1, fakeRun, store, false, nil, ""); err != nil {
		t.Fatal(err)
	}
	p1, _ := store.Read(keyB)

	var calls int64
	if _, _, _, err := runBenchmark(context.Background(), probes, before, after, 1, 1, countingFakeRun(&calls), store, true, nil, ""); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Errorf("--no-cache must re-run both sides fresh, got %d", calls)
	}
	p2, _ := store.Read(keyB)
	if len(p2) != len(p1)+1 {
		t.Errorf("--no-cache must still write through: pool %d -> %d", len(p1), len(p2))
	}
}

func TestCache_VolatileSideNeverCached(t *testing.T) {
	store := tc(t)
	probes := []dsl.Probe{poolTestProbes()[0]}
	vAfter := Env{Ref: "after", Volatile: true}
	keyBefore, _ := store.Key(before.Ref, "", taskPrompt(probes[0]))
	keyAfter, _ := store.Key(vAfter.Ref, "", taskPrompt(probes[0]))

	var calls int64
	if _, _, _, err := runBenchmark(context.Background(), probes, before, vAfter, 1, 1, countingFakeRun(&calls), store, false, nil, ""); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("run 1: want 2 runs (before miss + volatile after), got %d", calls)
	}
	if pool, _ := store.Read(keyAfter); len(pool) != 0 {
		t.Errorf("volatile side must not be persisted, found %d records", len(pool))
	}
	if pool, _ := store.Read(keyBefore); len(pool) != 1 {
		t.Errorf("committed before must cache, found %d records", len(pool))
	}

	if _, _, _, err := runBenchmark(context.Background(), probes, before, vAfter, 1, 1, countingFakeRun(&calls), store, false, nil, ""); err != nil {
		t.Fatal(err)
	}
	if extra := calls - 2; extra != 1 {
		t.Errorf("run 2: before must hit (0), volatile after must re-run (1); got %d new runs", extra)
	}
}

func TestCache_NoCacheIndependentArmsShareKey(t *testing.T) {
	store := tc(t)
	probes := []dsl.Probe{poolTestProbes()[0]}
	env := Env{Ref: "before"}
	key, _ := store.Key(env.Ref, "", taskPrompt(probes[0]))

	var calls int64
	if _, _, _, err := runBenchmark(context.Background(), probes, env, env, 1, 1, serialCountingRun(&calls), store, true, nil, ""); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("both arms must draw fresh (2 runs), got %d", calls)
	}
	pool, _ := store.Read(key)
	if len(pool) != 2 || pool[0].FinalText == pool[1].FinalText {
		t.Errorf("the two arms must be independent draws in the shared pool, got %d records %v", len(pool), pool)
	}
}
