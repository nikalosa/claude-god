package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/nikalosa/claude-god/internal/bashguard"
	"github.com/spf13/cobra"
)

var bashGuardCmd = &cobra.Command{
	Use:    "__bash-read-guard",
	Hidden: true,
	Args:   cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		raw, err := io.ReadAll(cmd.InOrStdin())
		if err != nil {
			block("cannot read hook input")
		}
		var in struct {
			ToolName  string `json:"tool_name"`
			ToolInput struct {
				Command string `json:"command"`
			} `json:"tool_input"`
		}
		if err := json.Unmarshal(raw, &in); err != nil {
			block("cannot parse hook input")
		}
		if in.ToolName != "Bash" {
			block("guard only vets Bash calls; got " + in.ToolName)
		}
		if allow, reason := bashguard.Classify(in.ToolInput.Command); !allow {
			block("read-only run: " + reason)
		}
		return nil
	},
}

func block(reason string) {
	fmt.Fprintln(os.Stderr, reason)
	os.Exit(2)
}

func init() {
	rootCmd.AddCommand(bashGuardCmd)
}
