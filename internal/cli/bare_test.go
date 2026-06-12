package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// nonTTY returns a real file handle (not a char device) so isTTY reads false.
func nonTTY(t *testing.T) *os.File {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "stdin")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	return f
}

func TestDiscoverCorpus(t *testing.T) {
	t.Run("override wins", func(t *testing.T) {
		got, err := discoverCorpus(t.TempDir(), "/x/y.yaml", nonTTY(t))
		if err != nil || got != "/x/y.yaml" {
			t.Fatalf("got %q, %v", got, err)
		}
	})

	t.Run("none points at quizgen", func(t *testing.T) {
		_, err := discoverCorpus(t.TempDir(), "", nonTTY(t))
		if err == nil || !strings.Contains(err.Error(), "quizgen") {
			t.Fatalf("want quizgen hint, got %v", err)
		}
	})

	t.Run("single is used", func(t *testing.T) {
		dir := t.TempDir()
		cdir := filepath.Join(dir, ".benchmark", "corpus")
		if err := os.MkdirAll(cdir, 0o755); err != nil {
			t.Fatal(err)
		}
		want := filepath.Join(cdir, "self.yaml")
		os.WriteFile(want, []byte("probes: []\n"), 0o644)
		got, err := discoverCorpus(dir, "", nonTTY(t))
		if err != nil || got != want {
			t.Fatalf("got %q, %v; want %q", got, err, want)
		}
	})

	t.Run("many non-TTY errors listing them", func(t *testing.T) {
		dir := t.TempDir()
		cdir := filepath.Join(dir, ".benchmark", "corpus")
		os.MkdirAll(cdir, 0o755)
		os.WriteFile(filepath.Join(cdir, "a.yaml"), []byte("probes: []\n"), 0o644)
		os.WriteFile(filepath.Join(cdir, "b.yaml"), []byte("probes: []\n"), 0o644)
		_, err := discoverCorpus(dir, "", nonTTY(t))
		if err == nil || !strings.Contains(err.Error(), "--corpus") {
			t.Fatalf("want a --corpus hint listing both, got %v", err)
		}
	})
}

func TestConfirm(t *testing.T) {
	if ok, err := confirm(true, nonTTY(t)); !ok || err != nil {
		t.Errorf("--yes should proceed, got %v %v", ok, err)
	}
	if _, err := confirm(false, nonTTY(t)); err == nil {
		t.Error("non-TTY without --yes must refuse")
	}
}
