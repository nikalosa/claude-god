// Command claude-validator A/B-benchmarks two CLAUDE.md context configurations
// for behavioral-fidelity regressions. See docs/PRD.md for the full design.
package main

import "github.com/nikalosa/claude-god/internal/cli"

func main() {
	cli.Execute()
}
