// Package report renders aggregated benchmark results. Markdown is the default
// (critical regressions first, then new passes, then cost/token/time deltas,
// then the rule x environment matrix); JSON is an option for CI pipelines.
//
// First markdown render lands in Issue #5.
package report
