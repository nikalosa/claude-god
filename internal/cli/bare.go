package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/nikalosa/claude-god/internal/autodetect"
	"github.com/nikalosa/claude-god/internal/dsl"
	"github.com/nikalosa/claude-god/internal/report"
	"github.com/nikalosa/claude-god/internal/snapshot"
)

var (
	flagEvalJudge       bool
	flagEvalKind        string
	flagEvalTarget      string
	flagEvalCorpus      string
	flagEvalBefore      string
	flagEvalAfter       string
	flagEvalSamples     int
	flagEvalConcurrency int
	flagEvalYes         bool
)

// defaultRunE is the bare `claude-benchmark` default action (ADR-0008): it
// auto-discovers the corpus, auto-detects Before/After from git, prints a spend
// plan, confirms, then runs the whole A/B benchmark and prints the report. Every
// flag is an optional override of an auto-detected default.
func defaultRunE(cmd *cobra.Command, _ []string) error {
	kinds, err := parseKinds(flagEvalKind)
	if err != nil {
		return err
	}
	if err := validateSamples(flagEvalSamples); err != nil {
		return err
	}
	if err := validateConcurrency(flagEvalConcurrency); err != nil {
		return err
	}
	target, err := filepath.Abs(flagEvalTarget)
	if err != nil {
		return fmt.Errorf("resolve --target: %w", err)
	}

	corpusPath, err := discoverCorpus(target, flagEvalCorpus, os.Stdin)
	if err != nil {
		return err
	}

	ctx := cmd.Context()
	res, err := autodetect.Resolve(ctx, target, flagEvalBefore, flagEvalAfter)
	if err != nil {
		return err
	}

	probes, err := dsl.LoadCorpus(corpusPath)
	if err != nil {
		return err
	}
	if len(probes) == 0 {
		return fmt.Errorf("corpus %s has no probes", corpusPath)
	}
	probes, err = filterByKind(probes, kinds)
	if err != nil {
		return err
	}

	j, err := judgeFor(probes, flagEvalJudge)
	if err != nil {
		return err
	}

	printPlan(os.Stderr, res, corpusPath, len(probes), flagEvalSamples, flagEvalConcurrency)
	ok, err := confirm(flagEvalYes, os.Stdin)
	if err != nil {
		return err
	}
	if !ok {
		fmt.Fprintln(os.Stderr, "aborted.")
		return nil
	}

	memSrc, err := memorySourceFor(target)
	if err != nil {
		return err
	}
	before := Env{Ref: res.Before}
	after := Env{Ref: res.After}
	run, cleanup, err := sharedRun(ctx, target, memPolicy{source: memSrc}, before, after)
	if err != nil {
		return err
	}
	defer cleanup()

	verdicts, prefs, aggs, err := runBenchmark(ctx, probes, before, after, flagEvalSamples, flagEvalConcurrency, run, j, "")
	if err != nil {
		return err
	}
	fmt.Println(report.RenderMarkdown(verdicts, prefs, aggs, flagEvalConcurrency))
	return nil
}

// discoverCorpus finds the corpus under <target>/.benchmark/corpus/: one file is
// used, several prompt a choice on a TTY (else error listing them), none points
// the dev at quizgen. override short-circuits the search.
func discoverCorpus(target, override string, in *os.File) (string, error) {
	if override != "" {
		return override, nil
	}
	dir := filepath.Join(target, ".benchmark", "corpus")
	matches, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
	if err != nil {
		return "", err
	}
	sort.Strings(matches)
	switch len(matches) {
	case 0:
		return "", fmt.Errorf("no corpus in %s — author one with the quizgen skill, or pass --corpus <file>", dir)
	case 1:
		return matches[0], nil
	default:
		if !isTTY(in) {
			return "", fmt.Errorf("multiple corpora in %s; pass --corpus <file>:\n  %s", dir, strings.Join(matches, "\n  "))
		}
		return chooseCorpus(matches, in)
	}
}

func chooseCorpus(matches []string, in *os.File) (string, error) {
	fmt.Fprintln(os.Stderr, "Multiple corpora found:")
	for i, m := range matches {
		fmt.Fprintf(os.Stderr, "  %d) %s\n", i+1, m)
	}
	fmt.Fprintf(os.Stderr, "Choose [1-%d]: ", len(matches))
	line, _ := bufio.NewReader(in).ReadString('\n')
	n, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil || n < 1 || n > len(matches) {
		return "", fmt.Errorf("no corpus chosen")
	}
	return matches[n-1], nil
}

func printPlan(w io.Writer, res autodetect.Resolution, corpus string, probes, samples, concurrency int) {
	fmt.Fprintln(w, "Benchmark plan")
	fmt.Fprintf(w, "  Before:      %s\n", res.BeforeDesc)
	fmt.Fprintf(w, "  After:       %s\n", res.AfterDesc)
	fmt.Fprintf(w, "  Corpus:      %s (%d probe(s))\n", corpus, probes)
	fmt.Fprintf(w, "  Samples:     %d per environment\n", samples)
	fmt.Fprintf(w, "  Concurrency: %d\n", concurrency)
	fmt.Fprintf(w, "  Runs:        %d claude -p calls (%d probes × %d samples × 2 envs)\n",
		probes*samples*2, probes, samples)
}

// confirm gates spend: --yes proceeds; a TTY prompts; a non-TTY without --yes
// refuses rather than spend silently.
func confirm(yes bool, in *os.File) (bool, error) {
	if yes {
		return true, nil
	}
	if !isTTY(in) {
		return false, fmt.Errorf("refusing to spend without confirmation (stdin is not a TTY) — pass --yes")
	}
	fmt.Fprint(os.Stderr, "Proceed? [y/N] ")
	line, _ := bufio.NewReader(in).ReadString('\n')
	resp := strings.ToLower(strings.TrimSpace(line))
	return resp == "y" || resp == "yes", nil
}

func memorySourceFor(target string) (string, error) {
	dir, err := snapshot.MemoryDir(target)
	if err != nil {
		return "", err
	}
	if fi, statErr := os.Stat(dir); statErr != nil || !fi.IsDir() {
		return "", nil
	}
	return dir, nil
}

func isTTY(in *os.File) bool {
	fi, err := in.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

func init() {
	rootCmd.RunE = defaultRunE
	rootCmd.Args = cobra.NoArgs
	f := rootCmd.Flags()
	f.BoolVar(&flagEvalJudge, "judge", false, "build the Judge for open-ended/plan/judge_rubric corpora (adds claude -p calls — extra spend)")
	f.StringVar(&flagEvalKind, "kind", allKinds, "probe kinds to run (CSV of rule_based,open_ended,plan)")
	f.StringVar(&flagEvalTarget, "target", ".", "path to the target repo under test")
	f.StringVar(&flagEvalCorpus, "corpus", "", "corpus YAML (default: auto-discover .benchmark/corpus/*.yaml)")
	f.StringVar(&flagEvalBefore, "before", "", "baseline committish (default: auto-detect from git)")
	f.StringVar(&flagEvalAfter, "after", "", "committish under test (default: auto-detect from git)")
	f.IntVar(&flagEvalSamples, "samples", 1, "samples per environment (odd N; default 1)")
	f.IntVar(&flagEvalConcurrency, "concurrency", 8, "max runs in flight (>=1; Duration is advisory above 1)")
	f.BoolVar(&flagEvalYes, "yes", false, "skip the spend-plan confirmation prompt")
}
