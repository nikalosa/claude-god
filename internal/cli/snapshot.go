package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/nikalosa/claude-god/internal/snapshot"
)

var (
	flagSnapTarget string
	flagSnapNoMem  bool
)

var snapshotCmd = &cobra.Command{
	Use:   "snapshot <name>",
	Short: "Pin the target's Environment to a benchmark/<name> branch",
	Long: `snapshot captures the target repo's Environment as an immutable
benchmark/<name> branch that run/calibrate consume: the committed HEAD tree
(CLAUDE.md, Claude rules, docs) plus, by default, the project memory copied into
.benchmark/memory-snapshot. Re-snapshotting the same name overwrites the branch.
Commit your Environment edits first — the snapshot reflects HEAD.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		branch, err := snapshot.Create(cmd.Context(), snapshot.Opts{
			TargetRepo:    flagSnapTarget,
			Name:          args[0],
			IncludeMemory: !flagSnapNoMem,
		})
		if err != nil {
			return err
		}
		fmt.Printf("created %s\n", branch)
		fmt.Printf("use it with: claude-benchmark run --before %s --after <other> ...\n", branch)
		return nil
	},
}

func init() {
	f := snapshotCmd.Flags()
	f.StringVar(&flagSnapTarget, "target", ".", "path to the target repo")
	f.BoolVar(&flagSnapNoMem, "no-memory-snapshot", false, "skip pinning project memory into the branch")
	rootCmd.AddCommand(snapshotCmd)
}
