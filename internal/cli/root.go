package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var version = "dev"

var rootCmd = &cobra.Command{
	Use:     "claude-benchmark",
	Version: version,
	Short:   "A/B-benchmark CLAUDE.md context configs for behavioral-fidelity regressions",
	Long: `claude-benchmark runs an A/B benchmark of two Claude Code context
configurations (before vs after a restructure) against a corpus of probes,
grades each rule pass/fail, and reports behavioral-fidelity and cost deltas.

Run bare (no subcommand) to benchmark the current repo end-to-end: it
auto-discovers the corpus, auto-detects Before/After from git, and prints the
report. assess scores one config (no A/B); run/snapshot/calibrate remain for
power users. See docs/PRD.md.`,
	SilenceUsage:  true,
	SilenceErrors: true,
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.AddCommand(runCmd)
}
