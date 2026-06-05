package detection

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nikalosa/claude-god/internal/aggregator"
	"github.com/nikalosa/claude-god/internal/dsl"
	"github.com/nikalosa/claude-god/internal/harness"
	"github.com/nikalosa/claude-god/internal/parser"
	"github.com/nikalosa/claude-god/internal/report"
	"github.com/nikalosa/claude-god/internal/runner"
)

func loadProbes(t *testing.T) []dsl.Probe {
	t.Helper()
	probes, err := dsl.LoadCorpus("testdata/corpus.yaml")
	if err != nil {
		t.Fatalf("LoadCorpus: %v", err)
	}
	return probes
}

func recsWith(text string, n int) []*parser.RunRecord {
	out := make([]*parser.RunRecord, n)
	for i := range out {
		out[i] = &parser.RunRecord{FinalText: text}
	}
	return out
}

// assertDetected is the core credibility assertion: the dropped rule shows as a
// Regression, while the untouched rule does NOT regress (disagreement is
// tolerated — we never require it to be exactly Stable).
func assertDetected(t *testing.T, verdicts []aggregator.Verdict) {
	t.Helper()
	status := map[string]aggregator.Status{}
	for _, v := range verdicts {
		status[v.RuleID] = v.Status
	}
	if status["names_mascot"] != aggregator.Regression {
		t.Errorf("dropped rule names_mascot: got %v, want Regression", status["names_mascot"])
	}
	if status["money_string"] == aggregator.Regression {
		t.Errorf("untouched rule money_string must not regress, got %v", status["money_string"])
	}
}

func sectionContains(md, heading, want string) bool {
	start := strings.Index(md, heading)
	if start < 0 {
		return false
	}
	rest := md[start+len(heading):]
	if end := strings.Index(rest, "\n## "); end >= 0 {
		rest = rest[:end]
	}
	return strings.Contains(rest, want)
}

// TestDetection_Pure proves the grade -> compare -> render pipeline lights up
// red on a planted regression, deterministically (no live claude -p).
func TestDetection_Pure(t *testing.T) {
	before := map[string]string{
		"mascot": "The official mascot is Captain Zilworld.",
		"money":  "Monetary amounts are stored as strings.",
	}
	after := map[string]string{
		"mascot": "The project does not specify a mascot name.", // backing line dropped -> FAIL
		"money":  "Monetary amounts are stored as strings.",     // retained -> PASS
	}

	ctx := context.Background()
	var aggs []aggregator.AggregatedOutcome
	for _, p := range loadProbes(t) {
		agg, pref, err := runner.GradeProbe(ctx, p, recsWith(before[p.ID], 3), recsWith(after[p.ID], 3), nil)
		if err != nil {
			t.Fatalf("GradeProbe %s: %v", p.ID, err)
		}
		if pref != nil {
			t.Errorf("rule-based probe %s produced a preference", p.ID)
		}
		aggs = append(aggs, agg)
	}

	verdicts := aggregator.Compare(aggs)
	assertDetected(t, verdicts)

	md := report.RenderMarkdown(verdicts, nil, aggs, 1)
	if !sectionContains(md, "## Rules", "names_mascot") {
		t.Errorf("dropped rule should appear in the rule matrix:\n%s", md)
	}
	if !sectionContains(md, "## Rules", "regression") {
		t.Errorf("dropped rule should be flagged 'regression' in the matrix:\n%s", md)
	}
}

// TestDetection_Live builds a real degraded Environment and runs claude -p
// against it. Gated behind CLAUDE_VALIDATOR_DOGFOOD=1 (live, costs money).
func TestDetection_Live(t *testing.T) {
	if os.Getenv("CLAUDE_VALIDATOR_DOGFOOD") != "1" {
		t.Skip("set CLAUDE_VALIDATOR_DOGFOOD=1 to run")
	}

	repo := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	install := func(fixture string) {
		t.Helper()
		data, err := os.ReadFile(filepath.Join("testdata", fixture))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(repo, "CLAUDE.md"), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	git("init", "-b", "main")
	install("claude-good.md")
	git("add", "-A")
	git("commit", "-m", "good environment")
	git("checkout", "-b", "degraded")
	install("claude-degraded.md")
	git("add", "-A")
	git("commit", "-m", "drop the mascot rule")
	git("checkout", "main")

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	var aggs []aggregator.AggregatedOutcome
	for _, p := range loadProbes(t) {
		before := sample(t, ctx, repo, "main", p)
		after := sample(t, ctx, repo, "degraded", p)
		agg, _, err := runner.GradeProbe(ctx, p, before, after, nil)
		if err != nil {
			t.Fatalf("GradeProbe %s: %v", p.ID, err)
		}
		aggs = append(aggs, agg)
	}

	verdicts := aggregator.Compare(aggs)
	t.Logf("report:\n%s", report.RenderMarkdown(verdicts, nil, aggs, 1))
	assertDetected(t, verdicts)
}

func sample(t *testing.T, ctx context.Context, repo, branch string, p dsl.Probe) []*parser.RunRecord {
	t.Helper()
	const n = 3
	recs := make([]*parser.RunRecord, 0, n)
	for i := 0; i < n; i++ {
		res, err := harness.Run(ctx, harness.Opts{
			TargetRepo:    repo,
			Branch:        branch,
			Prompt:        p.Prompt,
			NoMemSnapshot: true,
		})
		if err != nil {
			t.Fatalf("harness %s/%s sample %d: %v", p.ID, branch, i+1, err)
		}
		recs = append(recs, res.Record)
	}
	return recs
}
