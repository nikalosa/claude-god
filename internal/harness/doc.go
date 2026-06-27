// Package harness runs read-only benchmark probes against a target repo. Prepare
// checks one ref into a git worktree and swaps in the project-memory snapshot
// (unless opted out); RunIn invokes `claude -p` headless inside that worktree and
// parses the stream-json into a RunRecord; Close force-removes the worktree and
// restores memory. One worktree is shared by every Run of a ref (ADR-0015): runs
// are read-only (ADR-0006), so a shared cwd is safe and checkout cost is fixed,
// not linear in run count.
//
// Repo-agnostic. Implemented in Issue #4.
package harness
