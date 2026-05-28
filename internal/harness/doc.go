// Package harness runs a single benchmark probe in isolation: it spawns a fresh
// git worktree off the target branch, swaps in the project-memory snapshot
// (unless opted out), invokes `claude -p` headless inside the worktree, captures
// the stream-json and the resulting git diff as independent artifacts, then
// force-removes the worktree.
//
// Repo-agnostic. Implemented in Issue #4.
package harness
