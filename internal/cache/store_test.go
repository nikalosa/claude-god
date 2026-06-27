package cache

import (
	"fmt"
	"sync"
	"testing"

	"github.com/nikalosa/claude-god/internal/parser"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	return New(Opts{
		Root:            t.TempDir(),
		Model:           "claude-opus-4-8",
		Effort:          "medium",
		CLIVersionKey:   "2.1.195",
		CLIVersionStamp: "2.1.195",
		MemTag:          "none",
		Concurrency:     8,
		Resolve:         func(ref, mcp string) (string, string, error) { return ref, mcp, nil },
	})
}

func rec(text string) *parser.RunRecord {
	return &parser.RunRecord{FinalText: text, TotalCost: 0.5}
}

// TestStore_ReadMissing: an unseen fingerprint is an empty pool, not an error —
// the lookup path treats it as "all misses".
func TestStore_ReadMissing(t *testing.T) {
	s := testStore(t)
	pool, err := s.Read("deadbeef")
	if err != nil {
		t.Fatalf("missing key must not error: %v", err)
	}
	if len(pool) != 0 {
		t.Errorf("missing key must be empty, got %d", len(pool))
	}
}

// TestStore_AppendReadRoundTrip: appended records come back in order with their
// payload intact and the honesty stamps (CLI version + measured concurrency)
// applied at persist time.
func TestStore_AppendReadRoundTrip(t *testing.T) {
	s := testStore(t)
	key, err := s.Key("before", "", "prompt")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if err := s.Append(key, rec(fmt.Sprintf("r%d", i))); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}
	pool, err := s.Read(key)
	if err != nil {
		t.Fatal(err)
	}
	if len(pool) != 3 {
		t.Fatalf("want 3, got %d", len(pool))
	}
	for i, r := range pool {
		if r.FinalText != fmt.Sprintf("r%d", i) {
			t.Errorf("pool[%d] = %q, want r%d (append order not preserved)", i, r.FinalText, i)
		}
		if r.TotalCost != 0.5 {
			t.Errorf("pool[%d] lost payload: cost %v", i, r.TotalCost)
		}
		if r.CLIVersion != "2.1.195" || r.Concurrency != 8 {
			t.Errorf("pool[%d] missing stamps: cli=%q conc=%d", i, r.CLIVersion, r.Concurrency)
		}
	}
}

// TestStore_ReadNumericOrder: the pool is ordered by index, and the order is
// numeric not lexical — pool[10] must follow pool[9], not pool[1]. pool[0]
// stability across re-reads is load-bearing for the Preference comparison.
func TestStore_ReadNumericOrder(t *testing.T) {
	s := testStore(t)
	key, _ := s.Key("before", "", "p")
	for i := 0; i < 12; i++ {
		if err := s.Append(key, rec(fmt.Sprintf("%d", i))); err != nil {
			t.Fatal(err)
		}
	}
	pool, _ := s.Read(key)
	if len(pool) != 12 {
		t.Fatalf("want 12, got %d", len(pool))
	}
	for i, r := range pool {
		if r.FinalText != fmt.Sprintf("%d", i) {
			t.Fatalf("index %d out of numeric order: got %q", i, r.FinalText)
		}
	}
}

// TestStore_ConcurrentAppend: distinct sample indices map to distinct files, so
// concurrent writers to one pool never overwrite each other (ADR-0016: no
// hot-path lock, only index reservation). Run with -race.
func TestStore_ConcurrentAppend(t *testing.T) {
	s := testStore(t)
	key, _ := s.Key("before", "", "p")
	const n = 16
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs <- s.Append(key, rec(fmt.Sprintf("%d", i)))
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent append: %v", err)
		}
	}
	pool, _ := s.Read(key)
	if len(pool) != n {
		t.Fatalf("want %d distinct records (no overwrite), got %d", n, len(pool))
	}
	seen := map[string]bool{}
	for _, r := range pool {
		if seen[r.FinalText] {
			t.Errorf("duplicate record %q — an index collision overwrote a file", r.FinalText)
		}
		seen[r.FinalText] = true
	}
}

// TestStore_Key: the key is stable for equal inputs and distinct for any change
// in ref, MCP, or prompt (the per-(env,probe) varying inputs the store resolves).
func TestStore_Key(t *testing.T) {
	s := testStore(t)
	k, _ := s.Key("before", "", "p")
	again, _ := s.Key("before", "", "p")
	if k != again {
		t.Error("same inputs must yield the same key")
	}
	for name, mk := range map[string]func() string{
		"ref":    func() string { x, _ := s.Key("after", "", "p"); return x },
		"mcp":    func() string { x, _ := s.Key("before", `{"mcpServers":{}}`, "p"); return x },
		"prompt": func() string { x, _ := s.Key("before", "", "q"); return x },
	} {
		if mk() == k {
			t.Errorf("changing %s must change the key", name)
		}
	}
}

// TestStore_KeyReflectsEnvConstants: two stores differing only in a global key
// input (model/effort/CLI-version key/memory tag) must produce different keys
// for the same (ref, prompt) — these are environment-level, not probe-level.
func TestStore_KeyReflectsEnvConstants(t *testing.T) {
	base := testStore(t)
	k, _ := base.Key("before", "", "p")
	variants := map[string]Opts{
		"model":  {Model: "x"},
		"effort": {Effort: "x"},
		"cliKey": {CLIVersionKey: "x"},
		"memTag": {MemTag: "x"},
	}
	for name, override := range variants {
		o := Opts{
			Root: t.TempDir(), Model: "claude-opus-4-8", Effort: "medium",
			CLIVersionKey: "2.1.195", MemTag: "none",
			Resolve: func(ref, mcp string) (string, string, error) { return ref, mcp, nil },
		}
		switch name {
		case "model":
			o.Model = override.Model
		case "effort":
			o.Effort = override.Effort
		case "cliKey":
			o.CLIVersionKey = override.CLIVersionKey
		case "memTag":
			o.MemTag = override.MemTag
		}
		other, _ := New(o).Key("before", "", "p")
		if other == k {
			t.Errorf("changing %s must change the key", name)
		}
	}
}
