// Package judge wraps Anthropic API calls used as the grading escape hatch:
// L2 rubric scoring of open-ended architectural answers and L3 plan-vs-plan
// diffs. Isolated from the deterministic path so the rest of the system stays
// free of judge-LLM run-to-run noise.
//
// Out of the v1 first slice (L1 is regex-only); lands when L2 begins.
package judge
