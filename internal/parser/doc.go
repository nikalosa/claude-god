// Package parser converts the raw `claude -p --output-format stream-json` JSONL
// event stream into a structured RunRecord: ordered top-level tool calls,
// per-turn timing, session-aggregate token usage, and total cost.
//
// The event shape this package is built against is documented in
// testdata/stream-json-shape.md.
package parser
