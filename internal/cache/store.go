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

type Store struct {
	root        string
	model       string
	effort      string
	cliKey      string
	cliStamp    string
	memTag      string
	concurrency int
	resolve     func(ref, mcpConfig string) (sha, mcp string, err error)

	mu    sync.Mutex
	locks map[string]*sync.Mutex
	shas  map[string]resolved
}

type resolved struct {
	sha, mcp string
	err      error
}

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
		return "", nil
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
