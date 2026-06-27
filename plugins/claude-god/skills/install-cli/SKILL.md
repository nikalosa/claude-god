---
name: install-cli
description: Install the claude-benchmark CLI when it is missing, so config-bench can run. Detect-then-bootstrap — check PATH, else download the checksum-verified prebuilt binary, else go install. Use when claude-benchmark is "command not found", the dev says "set up / install the CLI", or config-bench finds no claude-benchmark on PATH.
---

# Bootstrap the claude-benchmark CLI

config-bench drives the `claude-benchmark` binary. The **plugin** channel ships a wrapper that self-installs it on first use; the **npx** channel installs skills only — no binary. This skill closes that gap: **bootstrap** the binary onto PATH, then hand back to the benchmark the dev was after.

Done when `claude-benchmark --version` prints a version. Steps 1–4 escalate toward that; stop at the first that reaches it.

## Steps

1. **Already there?** Run `claude-benchmark --version`. Prints a version → it's installed; skip to step 5.
2. **Bootstrap the prebuilt binary.** Run `scripts/install-cli.sh` (this skill's folder). It detects os/arch, downloads the matching GitHub release asset, verifies its sha256, and installs to `~/.local/bin` (override via `CLAUDE_BENCHMARK_BINDIR`). Done when it exits 0 and prints `installed: <path>`.
3. **On PATH?** Re-run `claude-benchmark --version`. Still "command not found" → the install dir is off PATH; the script printed an `export PATH=…` line. Have the dev add it to their shell rc (persists), or run that export now (this session). Done when the version prints.
4. **Fallback — script exited non-zero.** Exit 2 = unsupported os/arch; exit 1 = download/checksum failure. If `go` is on PATH: `go install github.com/nikalosa/claude-god/cmd/claude-benchmark@latest` (lands in `$(go env GOPATH)/bin` — step 3 applies if that's off PATH). No Go → point the dev at https://github.com/nikalosa/claude-god/releases to grab the asset by hand.
5. **Hand back.** Binary resolves → continue the run the dev asked for via **config-bench**.
