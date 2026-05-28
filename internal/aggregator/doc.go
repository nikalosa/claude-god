// Package aggregator combines the N=3 run records for a probe into one result:
// median statistics for cost/tokens/time and majority-vote (>=2/3) per-rule
// PASS/FAIL. Adaptive expansion to N=5 on critical-rule disagreement is a
// documented seam, deferred out of the v1 first slice.
//
// Implemented in Issue #6.
package aggregator
