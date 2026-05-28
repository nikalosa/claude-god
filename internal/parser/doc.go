// Package parser converts the raw `claude -p --output-format stream-json` JSONL
// event stream into a structured RunRecord: ordered tool calls (with per-call
// token attribution including sub-agent recursion), file mutations, per-turn
// timing, and total cost.
//
// The event shape this package is built against is documented in
// testdata/stream-json-shape.md, captured by the Issue #2 spike. Implemented in
// Issue #3 (flat case first; sub-agent recursion deferred behind a seam).
package parser
