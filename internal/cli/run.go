package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

var (
	flagLevel        string
	flagTarget       string
	flagCorpus       string
	flagNoMemSnapshot bool
)

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Run the A/B benchmark for the given tiers",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Printf("not implemented yet (level=%q target=%q corpus=%q no-memory-snapshot=%t)\n",
			flagLevel, flagTarget, flagCorpus, flagNoMemSnapshot)
		return nil
	},
}

func init() {
	f := runCmd.Flags()
	f.StringVar(&flagLevel, "level", "l1", "comma-separated tiers to run (l1,l2,l3,l4)")
	f.StringVar(&flagTarget, "target", ".", "path to the target repo under test")
	f.StringVar(&flagCorpus, "corpus", "", "path to the probe corpus directory")
	f.BoolVar(&flagNoMemSnapshot, "no-memory-snapshot", false, "skip pinning project memory into the run")
}
