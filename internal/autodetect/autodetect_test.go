package autodetect

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mustGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := git(context.Background(), dir, nil, args...)
	if err != nil {
		t.Fatalf("git %s: %v", strings.Join(args, " "), err)
	}
	return out
}

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func newRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	mustGit(t, dir, "init", "-b", "main")
	write(t, dir, "CLAUDE.md", "rule one\n")
	mustGit(t, dir, "add", "-A")
	mustGit(t, dir, "commit", "-m", "init")
	return dir
}

func TestResolve_CleanOnMain(t *testing.T) {
	dir := newRepo(t)
	_, err := Resolve(context.Background(), dir, "", "")
	if err == nil {
		t.Fatal("want error on a clean default branch")
	}
	if !strings.Contains(err.Error(), "--before") {
		t.Errorf("error should point at --before, got %v", err)
	}
}

func TestResolve_FeatureBranch(t *testing.T) {
	dir := newRepo(t)
	forkPoint := mustGit(t, dir, "rev-parse", "HEAD")
	mustGit(t, dir, "checkout", "-b", "feature")
	write(t, dir, "CLAUDE.md", "rule one\nrule two\n")
	mustGit(t, dir, "commit", "-am", "feature work")
	head := mustGit(t, dir, "rev-parse", "HEAD")

	res, err := Resolve(context.Background(), dir, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Dirty {
		t.Error("clean tree reported dirty")
	}
	if res.Before != forkPoint {
		t.Errorf("Before = %s, want fork point %s", res.Before, forkPoint)
	}
	if res.After != head {
		t.Errorf("After = %s, want HEAD %s", res.After, head)
	}
	if res.AfterVolatile {
		t.Error("a committed HEAD After must not be volatile (it is cacheable)")
	}
}

func TestResolve_Dirty(t *testing.T) {
	dir := newRepo(t)
	head := mustGit(t, dir, "rev-parse", "HEAD")
	write(t, dir, "CLAUDE.md", "rule one\nMUTATED\n")
	write(t, dir, "NEW.md", "brand new untracked\n")

	res, err := Resolve(context.Background(), dir, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Dirty {
		t.Error("dirty tree reported clean")
	}
	if res.Before != head {
		t.Errorf("Before = %s, want HEAD %s", res.Before, head)
	}
	if res.After == head {
		t.Fatal("After must differ from HEAD for a dirty tree")
	}
	if !res.AfterVolatile {
		t.Error("the working-tree snapshot After must be volatile (Run cache skips it)")
	}

	tree := mustGit(t, dir, "ls-tree", "-r", "--name-only", res.After)
	if !strings.Contains(tree, "NEW.md") {
		t.Errorf("After tree missing untracked NEW.md:\n%s", tree)
	}
	if got := mustGit(t, dir, "show", res.After+":CLAUDE.md"); !strings.Contains(got, "MUTATED") {
		t.Errorf("After tree missing uncommitted edit, got %q", got)
	}

	if now := mustGit(t, dir, "rev-parse", "HEAD"); now != head {
		t.Errorf("HEAD moved to %s (was %s)", now, head)
	}
	if out := mustGit(t, dir, "status", "--porcelain"); !strings.Contains(out, "CLAUDE.md") {
		t.Errorf("real working tree no longer dirty:\n%s", out)
	}
}

func TestResolve_Overrides(t *testing.T) {
	dir := newRepo(t)
	forkPoint := mustGit(t, dir, "rev-parse", "HEAD")
	mustGit(t, dir, "checkout", "-b", "feature")
	write(t, dir, "CLAUDE.md", "rule one\nrule two\n")
	mustGit(t, dir, "commit", "-am", "feature work")
	write(t, dir, "CLAUDE.md", "rule one\nrule two\ndirty\n")

	res, err := Resolve(context.Background(), dir, "main", "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Before != forkPoint {
		t.Errorf("Before = %s, want main %s", res.Before, forkPoint)
	}
	if !res.Dirty || res.After == "" {
		t.Errorf("After should be the dirty working-tree temp commit, got %q (dirty=%v)", res.After, res.Dirty)
	}
}
