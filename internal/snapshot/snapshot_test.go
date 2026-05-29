package snapshot

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

var ctx = context.Background()

func tgit(t *testing.T, repo string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// initRepo creates a committed target Environment: CLAUDE.md, a Claude rule, and
// a doc.
func initRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	tgit(t, repo, "init", "-b", "main")
	writeFile(t, filepath.Join(repo, "CLAUDE.md"), "# rules\n- be concise\n")
	writeFile(t, filepath.Join(repo, ".claude", "rules", "money.md"), "amounts as strings\n")
	writeFile(t, filepath.Join(repo, "docs", "design.md"), "design notes\n")
	tgit(t, repo, "add", "-A")
	tgit(t, repo, "commit", "-m", "initial environment")
	return repo
}

func lsTree(t *testing.T, repo, branch string) []string {
	t.Helper()
	out := tgit(t, repo, "ls-tree", "-r", "--name-only", branch)
	if out == "" {
		return nil
	}
	return strings.Split(out, "\n")
}

func has(files []string, want string) bool {
	for _, f := range files {
		if f == want {
			return true
		}
	}
	return false
}

func seedMemory(t *testing.T, repo string) {
	t.Helper()
	memDir, err := MemoryDir(repo)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(memDir, "MEMORY.md"), "- [fact](fact.md)\n")
	writeFile(t, filepath.Join(memDir, "fact.md"), "remember the migration script\n")
}

func TestCreate_WithMemory(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repo := initRepo(t)
	seedMemory(t, repo)

	branch, err := Create(ctx, Opts{TargetRepo: repo, Name: "before", IncludeMemory: true})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if branch != "validator/before" {
		t.Fatalf("branch = %q", branch)
	}

	files := lsTree(t, repo, branch)
	for _, want := range []string{
		"CLAUDE.md",
		".claude/rules/money.md",
		"docs/design.md",
		".validator/memory-snapshot/MEMORY.md",
		".validator/memory-snapshot/fact.md",
	} {
		if !has(files, want) {
			t.Errorf("branch %s missing %q; tree=%v", branch, want, files)
		}
	}
}

func TestCreate_NoMemory(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repo := initRepo(t)
	seedMemory(t, repo) // present, but opted out

	branch, err := Create(ctx, Opts{TargetRepo: repo, Name: "after", IncludeMemory: false})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	files := lsTree(t, repo, branch)
	if !has(files, "CLAUDE.md") {
		t.Errorf("environment files should still be captured; tree=%v", files)
	}
	for _, f := range files {
		if strings.Contains(f, "memory-snapshot") {
			t.Errorf("--no-memory-snapshot must not include memory, found %q", f)
		}
	}
}

func TestCreate_MemoryAbsentFallsBack(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // no memory seeded
	repo := initRepo(t)

	branch, err := Create(ctx, Opts{TargetRepo: repo, Name: "before", IncludeMemory: true})
	if err != nil {
		t.Fatalf("Create should not fail when memory is absent: %v", err)
	}
	files := lsTree(t, repo, branch)
	for _, f := range files {
		if strings.Contains(f, "memory-snapshot") {
			t.Errorf("no memory should be captured when none exists, found %q", f)
		}
	}
}

func TestCreate_Overwrite(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repo := initRepo(t)
	seedMemory(t, repo)

	if _, err := Create(ctx, Opts{TargetRepo: repo, Name: "before", IncludeMemory: true}); err != nil {
		t.Fatalf("first Create: %v", err)
	}
	first := tgit(t, repo, "rev-parse", "validator/before")

	// Change the environment and re-snapshot the same name.
	writeFile(t, filepath.Join(repo, "CLAUDE.md"), "# rules\n- be concise\n- new rule\n")
	tgit(t, repo, "commit", "-am", "edit environment")

	if _, err := Create(ctx, Opts{TargetRepo: repo, Name: "before", IncludeMemory: true}); err != nil {
		t.Fatalf("re-snapshot: %v", err)
	}
	second := tgit(t, repo, "rev-parse", "validator/before")
	if first == second {
		t.Error("re-snapshot should overwrite the branch to a new commit")
	}
}

func TestCreate_BadName(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repo := initRepo(t)
	for _, name := range []string{"", "bad/name", "has space", ".."} {
		if _, err := Create(ctx, Opts{TargetRepo: repo, Name: name, IncludeMemory: false}); err == nil {
			t.Errorf("expected error for name %q", name)
		}
	}
}

func TestCreate_NotAGitRepo(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if _, err := Create(ctx, Opts{TargetRepo: t.TempDir(), Name: "before"}); err == nil {
		t.Error("expected error for a non-git target")
	}
}
