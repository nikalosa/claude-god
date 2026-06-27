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

type PrepareOpts struct {
	TargetRepo    string
	Ref           string
	NoMemSnapshot bool
	MemorySource  string
	Model         string
	Effort        string
}

type Worktree struct {
	path       string
	tmpdir     string
	targetRepo string
	model      string
	effort     string
	restoreMem func()
}

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
