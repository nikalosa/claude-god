package harness

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/nikalosa/claude-god/internal/parser"
)

type Opts struct {
	TargetRepo    string
	Branch        string
	Prompt        string
	NoMemSnapshot bool
}

type Result struct {
	Record       *parser.RunRecord
	StreamPath   string
	DiffPath     string
	DiffStatPath string
	WorktreePath string
}

func Run(ctx context.Context, opts Opts) (*Result, error) {
	if !filepath.IsAbs(opts.TargetRepo) {
		return nil, errors.New("TargetRepo must be absolute")
	}
	if opts.Branch == "" {
		return nil, errors.New("Branch required")
	}
	if opts.Prompt == "" {
		return nil, errors.New("Prompt required")
	}

	artifacts, err := os.MkdirTemp("", "claude-validator-run-*")
	if err != nil {
		return nil, fmt.Errorf("artifacts dir: %w", err)
	}

	wt := filepath.Join(artifacts, "wt")
	if err := gitC(ctx, opts.TargetRepo, "worktree", "add", "--detach", wt, opts.Branch); err != nil {
		return nil, fmt.Errorf("worktree add: %w", err)
	}
	defer func() {
		_ = gitC(context.Background(), opts.TargetRepo, "worktree", "remove", "--force", wt)
	}()

	if !opts.NoMemSnapshot {
		restore, err := swapMemorySnapshot(wt)
		if err != nil {
			return nil, fmt.Errorf("memory swap: %w", err)
		}
		defer restore()
	}

	streamPath := filepath.Join(artifacts, "stream.jsonl")
	if err := invokeClaude(ctx, wt, opts.Prompt, streamPath); err != nil {
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

	diffPath := filepath.Join(artifacts, "diff.patch")
	diffStatPath := filepath.Join(artifacts, "diff.stat")
	if err := captureDiff(ctx, wt, diffPath, diffStatPath); err != nil {
		return nil, fmt.Errorf("capture diff: %w", err)
	}

	return &Result{
		Record:       rec,
		StreamPath:   streamPath,
		DiffPath:     diffPath,
		DiffStatPath: diffStatPath,
		WorktreePath: wt,
	}, nil
}

func swapMemorySnapshot(worktree string) (restore func(), err error) {
	src := filepath.Join(worktree, ".validator", "memory-snapshot")
	if _, statErr := os.Stat(src); statErr != nil {
		if os.IsNotExist(statErr) {
			fmt.Fprintf(os.Stderr, "warning: no memory snapshot at %s; proceeding without swap\n", src)
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

func invokeClaude(ctx context.Context, cwd, prompt, streamPath string) error {
	out, err := os.Create(streamPath)
	if err != nil {
		return err
	}
	defer out.Close()

	cmd := exec.CommandContext(ctx, "claude", "-p", prompt,
		"--output-format", "stream-json",
		"--verbose",
		"--permission-mode", "bypassPermissions")
	cmd.Dir = cwd
	cmd.Stdout = out
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func captureDiff(ctx context.Context, worktree, diffPath, diffStatPath string) error {
	if err := gitC(ctx, worktree, "add", "-A"); err != nil {
		return fmt.Errorf("git add: %w", err)
	}
	if err := gitToFile(ctx, worktree, diffPath, "diff", "--cached"); err != nil {
		return err
	}
	if err := gitToFile(ctx, worktree, diffStatPath, "diff", "--cached", "--stat"); err != nil {
		return err
	}
	if err := gitC(ctx, worktree, "reset"); err != nil {
		return fmt.Errorf("git reset: %w", err)
	}
	return nil
}

func gitC(ctx context.Context, dir string, args ...string) error {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	cmd.Stdout = io.Discard
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func gitToFile(ctx context.Context, dir, path string, args ...string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	cmd.Stdout = f
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
