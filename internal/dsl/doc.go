// Package dsl loads the YAML predicate DSL and evaluates it against a RunRecord
// to produce per-rule PASS/FAIL. Primitives include bash_call_matches,
// wrote_file_matching, diff_added_regex, transcript_contains, and the
// combinators not/and/or and unless_path_matches.
//
// Pattern-first and deterministic by design — the judge LLM (package judge) is
// the escape hatch for genuinely open-ended probes. L1 regex grading lands in
// Issue #5; the broader DSL follows in later tiers.
package dsl
