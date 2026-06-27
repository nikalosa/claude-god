package harness

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/nikalosa/claude-god/internal/parser"
)

// worktreeMu serializes `git worktree add`/`remove` against the target repo's
// shared .git: concurrent runs would otherwise race on git's index lock. Held
// only across the millisecond-scale git call, never across the run itself.
var worktreeMu sync.Mutex

type Opts struct {
	TargetRepo    string
	Branch        string
	Prompt        string
	NoMemSnapshot bool
	MemorySource  string
	MCPConfig     string
	Model         string
	Effort        string
}

type Result struct {
	Record *parser.RunRecord
}

// PrepareOpts configures a per-ref worktree: the target repo, the committish to
// check out, the memory policy injected once for the ref's lifetime, and the
// model/effort every Run of this ref invokes claude with (controlled variables,
// keyed by the Run cache).
type PrepareOpts struct {
	TargetRepo    string
	Ref           string
	NoMemSnapshot bool
	MemorySource  string
	Model         string
	Effort        string
}

// Worktree is a checkout of one ref, shared by every Run of that ref (ADR-0015).
// Runs are read-only (ADR-0006), so a shared cwd is safe; the tree is created
// once by Prepare and torn down once by Close. RunIn is safe to call
// concurrently for one Worktree — claude keys session state by per-run UUID and
// never mutates the tree — but Prepare/Close on a handle are not.
type Worktree struct {
	path       string
	tmpdir     string
	targetRepo string
	model      string
	effort     string
	restoreMem func()
}

// Prepare checks ref out into a fresh worktree and injects its memory once. The
// returned Worktree is shared across all Runs of the ref; release it with Close.
func Prepare(ctx context.Context, opts PrepareOpts) (*Worktree, error) {
	if !filepath.IsAbs(opts.TargetRepo) {
		return nil, errors.New("TargetRepo must be absolute")
	}
	if opts.Ref == "" {
		return nil, errors.New("Ref required")
	}

	tmpdir, err := os.MkdirTemp("", "claude-benchmark-wt-*")
	if err != nil {
		return nil, fmt.Errorf("worktree dir: %w", err)
	}
	path := filepath.Join(tmpdir, "wt")

	// Only the worktree admin op races on the shared .git, so lock just that
	// (fast: --no-checkout writes no files) and run the slow checkout outside the
	// lock. The tree is shared by every Run of this ref (ADR-0015): runs are
	// read-only, so a shared cwd is safe, and session state is keyed per-run by
	// UUID, so concurrent runs never collide in the shared project slug.
	worktreeMu.Lock()
	addErr := gitC(ctx, opts.TargetRepo, "worktree", "add", "--no-checkout", "--detach", path, opts.Ref)
	worktreeMu.Unlock()
	if addErr != nil {
		_ = os.RemoveAll(tmpdir)
		return nil, fmt.Errorf("worktree add: %w", addErr)
	}
	if err := gitC(ctx, path, "reset", "--hard", "HEAD"); err != nil {
		_ = removeWorktree(opts.TargetRepo, path)
		_ = os.RemoveAll(tmpdir)
		return nil, fmt.Errorf("worktree checkout: %w", err)
	}

	w := &Worktree{path: path, tmpdir: tmpdir, targetRepo: opts.TargetRepo, model: opts.Model, effort: opts.Effort, restoreMem: func() {}}
	if src := memorySource(path, opts.MemorySource, opts.NoMemSnapshot); src != "" {
		restore, err := swapMemory(path, src)
		if err != nil {
			_ = removeWorktree(opts.TargetRepo, path)
			_ = os.RemoveAll(tmpdir)
			return nil, fmt.Errorf("memory swap: %w", err)
		}
		w.restoreMem = restore
	}
	return w, nil
}

// RunIn executes one read-only `claude -p` for prompt in the shared worktree and
// returns its parsed record. mcpCfg is per-run — Before and After can share a
// tree and differ only here; empty falls back to the ref's committed .mcp.json.
// Safe to call concurrently for one Worktree.
func (w *Worktree) RunIn(ctx context.Context, prompt, mcpCfg string) (*Result, error) {
	if prompt == "" {
		return nil, errors.New("Prompt required")
	}
	artifacts, err := os.MkdirTemp("", "claude-benchmark-run-*")
	if err != nil {
		return nil, fmt.Errorf("artifacts dir: %w", err)
	}
	defer os.RemoveAll(artifacts)

	mcp := mcpConfig(w.path, Opts{MCPConfig: mcpCfg})
	streamPath := filepath.Join(artifacts, "stream.jsonl")
	if err := invokeClaude(ctx, w.path, prompt, mcp, w.model, w.effort, streamPath); err != nil {
		return nil, fmt.Errorf("claude -p: %w", err)
	}

	f, err := os.Open(streamPath)
	if err != nil {
		return nil, fmt.Errorf("open stream: %w", err)
	}
	rec, parseErr := parser.Parse(f)
	_ = f.Close()
	if parseErr != nil {
		return nil, fmt.Errorf("parse stream: %w", parseErr)
	}
	if err := checkMCPHealth(declaredMCPServers(mcp), rec.MCPServers, rec.ToolCalls); err != nil {
		return nil, err
	}

	return &Result{Record: rec}, nil
}

// Close releases the worktree once all Runs have finished: it restores memory
// (removes the injected project slug) and removes the worktree. It must run
// after the run pool drains — a per-run Close would wipe the slug that sibling
// runs are still writing transcripts into (ADR-0015).
func (w *Worktree) Close() error {
	w.restoreMem()
	err := removeWorktree(w.targetRepo, w.path)
	_ = os.RemoveAll(w.tmpdir)
	return err
}

func removeWorktree(targetRepo, path string) error {
	worktreeMu.Lock()
	defer worktreeMu.Unlock()
	return gitC(context.Background(), targetRepo, "worktree", "remove", "--force", path)
}

// Run is the one-shot path: Prepare + a single RunIn + Close. Used for one-off
// invocations and the dogfood test; the benchmark pool shares a worktree across
// runs by driving Prepare/RunIn/Close directly.
func Run(ctx context.Context, opts Opts) (*Result, error) {
	wt, err := Prepare(ctx, PrepareOpts{
		TargetRepo:    opts.TargetRepo,
		Ref:           opts.Branch,
		NoMemSnapshot: opts.NoMemSnapshot,
		MemorySource:  opts.MemorySource,
		Model:         opts.Model,
		Effort:        opts.Effort,
	})
	if err != nil {
		return nil, err
	}
	defer wt.Close()
	return wt.RunIn(ctx, opts.Prompt, opts.MCPConfig)
}

// memorySource picks what to inject into the worktree: an explicit live source
// (the bare command's current project memory) wins; --no-memory-snapshot
// disables injection; otherwise the committed snapshot pinned in the worktree.
func memorySource(worktree, source string, noSnapshot bool) string {
	if source != "" {
		return source
	}
	if noSnapshot {
		return ""
	}
	return filepath.Join(worktree, ".benchmark", "memory-snapshot")
}

func swapMemory(worktree, src string) (restore func(), err error) {
	if _, statErr := os.Stat(src); statErr != nil {
		if os.IsNotExist(statErr) {
			fmt.Fprintf(os.Stderr, "warning: no project memory at %s; proceeding without injection\n", src)
			return func() {}, nil
		}
		return nil, statErr
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	slug := strings.ReplaceAll(worktree, "/", "-")
	dest := filepath.Join(home, ".claude", "projects", slug, "memory")
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return nil, err
	}
	if err := copyDir(src, dest); err != nil {
		return nil, fmt.Errorf("copy snapshot: %w", err)
	}
	return func() {
		_ = os.RemoveAll(filepath.Join(home, ".claude", "projects", slug))
	}, nil
}

// mcpConfig resolves which MCP servers this Environment carries: an explicit
// config (a --mcp-config file path or inline JSON) wins; otherwise the ref's
// committed .mcp.json if the worktree has one. Empty means no MCP. The result is
// always paired with --strict-mcp-config in invokeClaude, so MCP is a controlled
// variable — never the ambient user/global servers that default discovery leaks.
func mcpConfig(worktree string, opts Opts) string {
	if opts.MCPConfig != "" {
		return opts.MCPConfig
	}
	committed := filepath.Join(worktree, ".mcp.json")
	if _, err := os.Stat(committed); err == nil {
		return committed
	}
	return ""
}

func invokeClaude(ctx context.Context, cwd, prompt, mcp, model, effort, streamPath string) error {
	out, err := os.Create(streamPath)
	if err != nil {
		return err
	}
	defer out.Close()

	// Runs are read-only (ADR-0006): grading reads the assistant text, so the
	// model may inspect the env but must not mutate the tree, hit the network, or
	// open a browser. Edit/Write/WebFetch/Agent stay bare-name disallowed (which
	// holds even under bypassPermissions). Bash is re-enabled so the model loads
	// content the way a terminal does (cat/head/sed/grep slices, not whole-file
	// Reads) and constrained to read-only by a PreToolUse hook — the only lever
	// that survives bypassPermissions, where scoped Bash(...) rules do not.
	// --disable-slash-commands drops skills, which are not part of the Environment.
	// --strict-mcp-config makes MCP part of the Environment under test: only the
	// servers the caller pins load, never the dev's ambient user/global config.
	settings, err := readOnlyBashSettings()
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "claude", claudeArgs(prompt, mcp, settings, model, effort)...)
	cmd.Dir = cwd
	cmd.Env = SanitizedEnv(os.Environ())
	cmd.Stdout = out
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// checkMCPHealth fails a run whose declared MCP servers were not actually usable.
// declared is the server set the Environment pinned (from the --mcp-config); under
// --strict-mcp-config that is exactly what should load. A declared server is fine
// only if init reports it "connected" OR it made at least one mcp__<name>__* tool
// call (proof it loaded and was reachable). Anything else means the controlled
// variable silently went missing and grading the run would report a false "no
// difference":
//   - "disabled": disabled by name in the dev's ambient ~/.claude.json
//     (disabledMcpServers), which --strict-mcp-config does not override.
//   - "pending"/absent with zero calls: the headless client started its turn
//     before the stdio MCP handshake finished — the startup race under concurrency.
//
// "pending" is ambiguous on its own (it can still connect-and-be-used), so a tool
// call is the only positive proof; the run pool retries a failure at low
// concurrency, where the handshake wins.
func checkMCPHealth(declared []string, servers []parser.MCPServer, calls []parser.ToolCall) error {
	if len(declared) == 0 {
		return nil
	}
	status := make(map[string]string, len(servers))
	for _, s := range servers {
		status[s.Name] = s.Status
	}
	used := mcpCallCounts(calls)
	var bad []string
	for _, name := range declared {
		st := status[name]
		if st == "connected" || used[name] > 0 {
			continue
		}
		if st == "" {
			st = "absent"
		}
		bad = append(bad, fmt.Sprintf("%s=%s, 0 tool calls", name, st))
	}
	if len(bad) > 0 {
		return fmt.Errorf("declared MCP server(s) did not load for this run: %s — either disabled by name in ~/.claude.json (disabledMcpServers, which --strict-mcp-config does not re-enable; rename the server or drop it from that list) or lost the startup race under concurrency (the headless client began its turn before the stdio handshake finished)", strings.Join(bad, "; "))
	}
	return nil
}

// declaredMCPServers returns the server names a --mcp-config declares — inline
// JSON or a file path, both shaped {"mcpServers":{<name>:{...}}}. It is how the
// harness knows which servers *should* have loaded, so checkMCPHealth can catch a
// declared server that never appeared. An empty config or any parse failure yields
// no names (nothing to assert).
func declaredMCPServers(mcp string) []string {
	if mcp == "" {
		return nil
	}
	data := []byte(mcp)
	if !strings.HasPrefix(strings.TrimSpace(mcp), "{") {
		b, err := os.ReadFile(mcp)
		if err != nil {
			return nil
		}
		data = b
	}
	var cfg struct {
		MCPServers map[string]json.RawMessage `json:"mcpServers"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil
	}
	names := make([]string, 0, len(cfg.MCPServers))
	for name := range cfg.MCPServers {
		names = append(names, name)
	}
	return names
}

// mcpCallCounts tallies how many times each MCP server's tools were called, keyed
// by server name. MCP tool calls are named mcp__<server>__<tool>; the server
// segment is the proof a declared server actually loaded and was usable.
func mcpCallCounts(calls []parser.ToolCall) map[string]int {
	const prefix = "mcp__"
	counts := map[string]int{}
	for _, c := range calls {
		if !strings.HasPrefix(c.Name, prefix) {
			continue
		}
		rest := c.Name[len(prefix):]
		i := strings.Index(rest, "__")
		if i <= 0 {
			continue
		}
		counts[rest[:i]]++
	}
	return counts
}

// SanitizedEnv strips the launcher's Claude Code session/effort variables so the
// benchmarked `claude -p` runs in a controlled environment, not whatever the
// process that started the tool happened to carry. Without this, launching the
// tool from inside a Claude Code session leaks CLAUDE_EFFORT (reasoning effort),
// CLAUDE_CODE_SESSION_ID, CLAUDE_CODE_CHILD_SESSION, CLAUDECODE, etc. into every
// run — making results depend on where the tool was launched (non-reproducible).
// Denylist, not allowlist: PATH/HOME/TMPDIR/ANTHROPIC_* must pass through for
// claude to find its binary, config, and auth.
func SanitizedEnv(env []string) []string {
	out := env[:0:0]
	for _, kv := range env {
		name := kv
		if i := strings.IndexByte(kv, '='); i >= 0 {
			name = kv[:i]
		}
		if name == "CLAUDECODE" || name == "CLAUDE_EFFORT" || name == "AI_AGENT" ||
			strings.HasPrefix(name, "CLAUDE_CODE_") {
			continue
		}
		out = append(out, kv)
	}
	return out
}

// claudeArgs builds the headless claude -p argv. Split out so tests pin the
// read-only and MCP flags without shelling out. model/effort pin the model and
// reasoning effort as controlled variables the Run cache keys on; each is passed
// only when set, so the one-shot Run path keeps claude's configured defaults.
func claudeArgs(prompt, mcp, settings, model, effort string) []string {
	args := []string{"-p", prompt,
		"--output-format", "stream-json",
		"--verbose",
		"--permission-mode", "bypassPermissions",
		"--disallowedTools", "Agent", "Edit", "Write", "WebFetch",
		"--settings", settings,
		"--strict-mcp-config",
		"--disable-slash-commands"}
	if model != "" {
		args = append(args, "--model", model)
	}
	if effort != "" {
		args = append(args, "--effort", effort)
	}
	if mcp != "" {
		args = append(args, "--mcp-config", mcp)
	}
	return args
}

// readOnlyBashSettings returns an inline Claude Code settings JSON registering a
// PreToolUse hook on Bash that shells back into this binary's hidden
// __bash-read-guard subcommand. The guard blocks (exit 2) any command that is
// not provably read-only. Passed via --settings, it overrides the worktree's own
// settings (precedence 2) without editing the tree.
func readOnlyBashSettings() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("locate self for bash guard hook: %w", err)
	}
	settings := map[string]any{
		"hooks": map[string]any{
			"PreToolUse": []any{map[string]any{
				"matcher": "Bash",
				"hooks": []any{map[string]any{
					"type":    "command",
					"command": shellQuote(exe) + " __bash-read-guard",
				}},
			}},
		},
	}
	b, err := json.Marshal(settings)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func gitC(ctx context.Context, dir string, args ...string) error {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	cmd.Stdout = io.Discard
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}
		return copyFile(path, target, info.Mode())
	})
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}
