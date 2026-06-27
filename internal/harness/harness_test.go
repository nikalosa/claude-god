package harness

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/nikalosa/claude-god/internal/parser"
)

func initGitRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@e",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@e")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q")
	if err := os.WriteFile(filepath.Join(repo, "file.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", "-A")
	git("commit", "-q", "-m", "init")
	return repo
}

func TestPrepareClose(t *testing.T) {
	repo := initGitRepo(t)
	wt, err := Prepare(context.Background(), PrepareOpts{TargetRepo: repo, Ref: "HEAD", NoMemSnapshot: true})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if _, err := os.Stat(filepath.Join(wt.path, "file.txt")); err != nil {
		t.Errorf("worktree not checked out: %v", err)
	}
	if err := wt.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
	if _, err := os.Stat(wt.path); !os.IsNotExist(err) {
		t.Errorf("worktree not removed after Close: err=%v", err)
	}
	if _, err := os.Stat(wt.tmpdir); !os.IsNotExist(err) {
		t.Errorf("tmpdir not removed after Close: err=%v", err)
	}
}

func TestPrepareCloseMemory(t *testing.T) {
	repo := initGitRepo(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "MEMORY.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	wt, err := Prepare(context.Background(), PrepareOpts{TargetRepo: repo, Ref: "HEAD", MemorySource: src})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	slug := strings.ReplaceAll(wt.path, "/", "-")
	injected := filepath.Join(home, ".claude", "projects", slug, "memory", "MEMORY.md")
	if _, err := os.Stat(injected); err != nil {
		t.Errorf("memory not injected at %s: %v", injected, err)
	}
	if err := wt.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "projects", slug)); !os.IsNotExist(err) {
		t.Errorf("project slug not removed after Close: err=%v", err)
	}
}

func TestHarness_Dogfood(t *testing.T) {
	if os.Getenv("CLAUDE_BENCHMARK_DOGFOOD") != "1" {
		t.Skip("set CLAUDE_BENCHMARK_DOGFOOD=1 to run")
	}

	target, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 5; i++ {
		if _, err := os.Stat(target + "/go.mod"); err == nil {
			break
		}
		target += "/.."
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	res, err := Run(ctx, Opts{
		TargetRepo:    target,
		Branch:        "main",
		Prompt:        "In one sentence, what is the purpose of claude-benchmark?",
		NoMemSnapshot: true,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if res.Record == nil {
		t.Fatal("nil RunRecord")
	}
	if res.Record.FinalText == "" {
		t.Error("empty FinalText")
	}
	if res.Record.TotalCost <= 0 {
		t.Errorf("expected TotalCost > 0, got %v", res.Record.TotalCost)
	}
	rj, _ := json.MarshalIndent(res.Record, "", "  ")
	t.Logf("RunRecord:\n%s", rj)
}

func TestClaudeArgs(t *testing.T) {
	has := func(args []string, want string) bool {
		for _, a := range args {
			if a == want {
				return true
			}
		}
		return false
	}
	pairAt := func(args []string, flag, val string) bool {
		for i, a := range args {
			if a == flag && i+1 < len(args) && args[i+1] == val {
				return true
			}
		}
		return false
	}

	noMCP := claudeArgs("q", "", "{}", "", "")
	if !has(noMCP, "--strict-mcp-config") {
		t.Error("--strict-mcp-config must always be present")
	}
	if has(noMCP, "--mcp-config") {
		t.Error("--mcp-config must be absent when no config is given")
	}
	if has(noMCP, "--model") || has(noMCP, "--effort") {
		t.Error("--model/--effort must be absent when unset (one-shot Run keeps claude defaults)")
	}
	for _, f := range []string{"-p", "--disallowedTools", "--disable-slash-commands"} {
		if !has(noMCP, f) {
			t.Errorf("missing baseline flag %q", f)
		}
	}

	withMCP := claudeArgs("q", "/tmp/cg.json", "{}", "claude-opus-4-8", "medium")
	if !pairAt(withMCP, "--mcp-config", "/tmp/cg.json") {
		t.Errorf("expected --mcp-config /tmp/cg.json, got %v", withMCP)
	}
	if !pairAt(withMCP, "--model", "claude-opus-4-8") {
		t.Errorf("expected --model claude-opus-4-8, got %v", withMCP)
	}
	if !pairAt(withMCP, "--effort", "medium") {
		t.Errorf("expected --effort medium, got %v", withMCP)
	}
	if !has(withMCP, "--strict-mcp-config") {
		t.Error("--strict-mcp-config must stay present with a config")
	}
}

func TestMCPConfig(t *testing.T) {
	if got := mcpConfig("/no/such/wt", Opts{MCPConfig: "X"}); got != "X" {
		t.Errorf("explicit config must win, got %q", got)
	}

	wt := t.TempDir()
	if got := mcpConfig(wt, Opts{}); got != "" {
		t.Errorf("no config and no committed .mcp.json must resolve empty, got %q", got)
	}

	committed := wt + "/.mcp.json"
	if err := os.WriteFile(committed, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := mcpConfig(wt, Opts{}); got != committed {
		t.Errorf("must fall back to committed .mcp.json, got %q", got)
	}
	if got := mcpConfig(wt, Opts{MCPConfig: "X"}); got != "X" {
		t.Errorf("explicit config must still win over committed, got %q", got)
	}
}

func TestSanitizedEnv(t *testing.T) {
	in := []string{
		"PATH=/usr/bin",
		"HOME=/home/x",
		"TMPDIR=/var/folders/x/T/",
		"ANTHROPIC_API_KEY=sk-test",
		"CLAUDE_EFFORT=xhigh",
		"CLAUDECODE=1",
		"CLAUDE_CODE_SESSION_ID=abc",
		"CLAUDE_CODE_CHILD_SESSION=1",
		"CLAUDE_CODE_ENTRYPOINT=cli",
		"AI_AGENT=claude-code_2-1-181_agent",
	}
	got := SanitizedEnv(in)
	gotSet := map[string]bool{}
	for _, kv := range got {
		gotSet[kv] = true
	}
	for _, want := range []string{"PATH=/usr/bin", "HOME=/home/x", "TMPDIR=/var/folders/x/T/", "ANTHROPIC_API_KEY=sk-test"} {
		if !gotSet[want] {
			t.Errorf("must keep %q", want)
		}
	}
	for _, kv := range got {
		name := kv
		if i := strings.IndexByte(kv, '='); i >= 0 {
			name = kv[:i]
		}
		if name == "CLAUDECODE" || name == "CLAUDE_EFFORT" || name == "AI_AGENT" || strings.HasPrefix(name, "CLAUDE_CODE_") {
			t.Errorf("must strip leaked var %q", kv)
		}
	}
}

func TestCheckMCPHealth(t *testing.T) {
	call := func(server string) parser.ToolCall { return parser.ToolCall{Name: "mcp__" + server + "__search"} }
	cases := []struct {
		name     string
		declared []string
		servers  []parser.MCPServer
		calls    []parser.ToolCall
		wantErr  bool
	}{
		{"no mcp declared", nil, nil, nil, false},
		{"connected", []string{"cgtest"}, []parser.MCPServer{{Name: "cgtest", Status: "connected"}}, nil, false},
		{"pending but used", []string{"cgtest"}, []parser.MCPServer{{Name: "cgtest", Status: "pending"}}, []parser.ToolCall{call("cgtest")}, false},
		{"pending and unused (startup race)", []string{"cgtest"}, []parser.MCPServer{{Name: "cgtest", Status: "pending"}}, nil, true},
		{"disabled by name", []string{"codegraph"}, []parser.MCPServer{{Name: "codegraph", Status: "disabled"}}, nil, true},
		{"absent from init", []string{"cgtest"}, nil, nil, true},
		{"one declared server never loaded", []string{"ok", "codegraph"}, []parser.MCPServer{{Name: "ok", Status: "connected"}, {Name: "codegraph", Status: "disabled"}}, nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := checkMCPHealth(tc.declared, tc.servers, tc.calls)
			if (err != nil) != tc.wantErr {
				t.Fatalf("checkMCPHealth(%v, %v, %v) err=%v, wantErr=%v", tc.declared, tc.servers, tc.calls, err, tc.wantErr)
			}
			if tc.wantErr && err != nil && !strings.Contains(err.Error(), "did not load") {
				t.Errorf("error should name the failure mode, got %q", err)
			}
		})
	}
}

func TestDeclaredMCPServers(t *testing.T) {
	if got := declaredMCPServers(""); got != nil {
		t.Errorf("empty config must declare nothing, got %v", got)
	}
	inline := `{"mcpServers":{"cgtest":{"type":"stdio","command":"codegraph"}}}`
	if got := declaredMCPServers(inline); len(got) != 1 || got[0] != "cgtest" {
		t.Errorf("inline JSON must yield [cgtest], got %v", got)
	}
	p := t.TempDir() + "/mcp.json"
	if err := os.WriteFile(p, []byte(`{"mcpServers":{"a":{},"b":{}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got := declaredMCPServers(p)
	sort.Strings(got)
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("file config must yield [a b], got %v", got)
	}
}

func TestMCPCallCounts(t *testing.T) {
	got := mcpCallCounts([]parser.ToolCall{
		{Name: "Read"},
		{Name: "mcp__cgtest__search"},
		{Name: "mcp__cgtest__neighbors"},
		{Name: "mcp__other__x"},
		{Name: "mcp__malformed"},
	})
	if got["cgtest"] != 2 {
		t.Errorf("cgtest want 2, got %d", got["cgtest"])
	}
	if got["other"] != 1 {
		t.Errorf("other want 1, got %d", got["other"])
	}
	if _, ok := got["malformed"]; ok {
		t.Errorf("malformed mcp name must be ignored, got %v", got)
	}
}

func TestReadOnlyBashSettings(t *testing.T) {
	s, err := readOnlyBashSettings()
	if err != nil {
		t.Fatalf("readOnlyBashSettings: %v", err)
	}
	var got struct {
		Hooks struct {
			PreToolUse []struct {
				Matcher string `json:"matcher"`
				Hooks   []struct {
					Type    string `json:"type"`
					Command string `json:"command"`
				} `json:"hooks"`
			} `json:"PreToolUse"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal([]byte(s), &got); err != nil {
		t.Fatalf("settings is not valid JSON: %v\n%s", err, s)
	}
	if len(got.Hooks.PreToolUse) != 1 || got.Hooks.PreToolUse[0].Matcher != "Bash" {
		t.Fatalf("expected one PreToolUse hook matching Bash, got %+v", got.Hooks.PreToolUse)
	}
	h := got.Hooks.PreToolUse[0].Hooks
	if len(h) != 1 || h[0].Type != "command" || !strings.HasSuffix(h[0].Command, "__bash-read-guard") {
		t.Fatalf("expected a command hook ending in __bash-read-guard, got %+v", h)
	}
}
