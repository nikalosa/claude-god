package cache

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/nikalosa/claude-god/internal/parser"
)

// Store is the content-addressed Run cache: one directory per Fingerprint under
// Root, one zero-padded NNNN.json per Run in a directory. Distinct sample indices
// map to distinct files, so concurrent writers to one pool never touch the same
// file; a brief per-fingerprint lock is held only to reserve the next index.
type Store struct {
	root        string
	model       string
	effort      string
	cliKey      string // CLI version token folded into the Fingerprint
	cliStamp    string // true detected CLI version stamped on each record
	memTag      string
	concurrency int
	resolve     func(ref, mcpConfig string) (sha, mcp string, err error)

	mu    sync.Mutex
	locks map[string]*sync.Mutex
	shas  map[string]resolved // memoized (ref+mcp) -> resolution
}

type resolved struct {
	sha, mcp string
	err      error
}

// Opts configures a Store. Resolve is injectable so the fingerprint plumbing
// (git rev-parse for the SHA, the effective MCP bytes) is faked in tests; when
// nil it defaults to the real git-backed resolver rooted at Target.
type Opts struct {
	Root            string
	Model           string
	Effort          string
	CLIVersionKey   string
	CLIVersionStamp string
	MemTag          string
	Concurrency     int
	Target          string
	Resolve         func(ref, mcpConfig string) (sha, mcp string, err error)
}

func New(opts Opts) *Store {
	resolve := opts.Resolve
	if resolve == nil {
		resolve = gitResolver(opts.Target)
	}
	return &Store{
		root:        opts.Root,
		model:       opts.Model,
		effort:      opts.Effort,
		cliKey:      opts.CLIVersionKey,
		cliStamp:    opts.CLIVersionStamp,
		memTag:      opts.MemTag,
		concurrency: opts.Concurrency,
		resolve:     resolve,
		locks:       map[string]*sync.Mutex{},
		shas:        map[string]resolved{},
	}
}

// Key resolves a (ref, mcpConfig, runPrompt) triple to its Fingerprint. The ref
// is resolved to a SHA and the MCP config to its effective bytes (memoized per
// ref+config), then folded with the store's environment-level constants.
func (s *Store) Key(ref, mcpConfig, runPrompt string) (string, error) {
	sha, mcp, err := s.resolveCached(ref, mcpConfig)
	if err != nil {
		return "", err
	}
	return Fingerprint(Inputs{
		CommitSHA:  sha,
		MCPConfig:  mcp,
		MemTag:     s.memTag,
		Model:      s.model,
		Effort:     s.effort,
		CLIVersion: s.cliKey,
		RunPrompt:  runPrompt,
	}), nil
}

func (s *Store) resolveCached(ref, mcpConfig string) (string, string, error) {
	ck := ref + "\x00" + mcpConfig
	s.mu.Lock()
	r, ok := s.shas[ck]
	s.mu.Unlock()
	if ok {
		return r.sha, r.mcp, r.err
	}
	sha, mcp, err := s.resolve(ref, mcpConfig)
	s.mu.Lock()
	s.shas[ck] = resolved{sha, mcp, err}
	s.mu.Unlock()
	return sha, mcp, err
}

// Read returns the Sample pool for a Fingerprint, ordered by numeric index. A
// missing pool is empty, not an error (it means "all misses").
func (s *Store) Read(key string) ([]*parser.RunRecord, error) {
	dir := filepath.Join(s.root, key)
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	idxs := indexFiles(entries)
	sort.Slice(idxs, func(a, b int) bool { return idxs[a].n < idxs[b].n })

	pool := make([]*parser.RunRecord, 0, len(idxs))
	for _, f := range idxs {
		b, err := os.ReadFile(filepath.Join(dir, f.name))
		if err != nil {
			return nil, err
		}
		var r parser.RunRecord
		if err := json.Unmarshal(b, &r); err != nil {
			return nil, fmt.Errorf("corrupt cache record %s: %w", filepath.Join(key, f.name), err)
		}
		pool = append(pool, &r)
	}
	return pool, nil
}

// Append persists one completed Run to a pool as the next index. The per-key
// lock spans index reservation through the atomic rename, so two concurrent
// writers cannot pick the same index. The record is stamped with the true CLI
// version and measured concurrency without mutating the caller's copy.
func (s *Store) Append(key string, r *parser.RunRecord) error {
	dir := filepath.Join(s.root, key)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	lk := s.lockFor(key)
	lk.Lock()
	defer lk.Unlock()

	idx, err := s.nextIndex(dir)
	if err != nil {
		return err
	}

	stamped := *r
	stamped.CLIVersion = s.cliStamp
	stamped.Concurrency = s.concurrency
	b, err := json.MarshalIndent(&stamped, "", "  ")
	if err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	final := filepath.Join(dir, fmt.Sprintf("%04d.json", idx))
	if err := os.Rename(tmpName, final); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}

func (s *Store) lockFor(key string) *sync.Mutex {
	s.mu.Lock()
	defer s.mu.Unlock()
	lk, ok := s.locks[key]
	if !ok {
		lk = &sync.Mutex{}
		s.locks[key] = lk
	}
	return lk
}

// nextIndex is max(existing index)+1, so a gap (e.g. a manually deleted record)
// never causes an overwrite.
func (s *Store) nextIndex(dir string) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, err
	}
	next := 0
	for _, f := range indexFiles(entries) {
		if f.n+1 > next {
			next = f.n + 1
		}
	}
	return next, nil
}

type indexFile struct {
	name string
	n    int
}

func indexFiles(entries []os.DirEntry) []indexFile {
	var out []indexFile
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		base, ok := strings.CutSuffix(name, ".json")
		if !ok || strings.HasPrefix(name, ".") {
			continue
		}
		n, err := strconv.Atoi(base)
		if err != nil {
			continue
		}
		out = append(out, indexFile{name, n})
	}
	return out
}

// gitResolver is the production resolver: the SHA from `git rev-parse <ref>` and
// the effective MCP bytes (an explicit config wins, else the ref's committed
// .mcp.json read straight from the object store so a fully-cached ref needs no
// checkout).
func gitResolver(target string) func(ref, mcpConfig string) (string, string, error) {
	return func(ref, mcpConfig string) (string, string, error) {
		sha, err := gitOut(target, "rev-parse", ref+"^{commit}")
		if err != nil {
			return "", "", fmt.Errorf("resolve ref %q to a SHA: %w", ref, err)
		}
		mcp, err := effectiveMCP(target, sha, mcpConfig)
		if err != nil {
			return "", "", err
		}
		return sha, mcp, nil
	}
}

func effectiveMCP(target, sha, mcpConfig string) (string, error) {
	if mcpConfig != "" {
		if strings.HasPrefix(strings.TrimSpace(mcpConfig), "{") {
			return mcpConfig, nil
		}
		b, err := os.ReadFile(mcpConfig)
		if err != nil {
			return "", fmt.Errorf("read MCP config %q: %w", mcpConfig, err)
		}
		return string(b), nil
	}
	committed, err := gitOut(target, "show", sha+":.mcp.json")
	if err != nil {
		return "", nil // no committed .mcp.json at this SHA = no MCP layer
	}
	return committed, nil
}

func gitOut(dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(context.Background(), "git", append([]string{"-C", dir}, args...)...)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(errb.String()))
	}
	return strings.TrimSpace(out.String()), nil
}
