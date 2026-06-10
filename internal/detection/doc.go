// Package detection holds the end-to-end proof that the benchmark DETECTS a
// genuinely dropped rule — closing the credibility gap that it had only ever
// been run Before-vs-identical-Before (correctly reporting "nothing changed").
//
// The deterministic test (TestDetection_Pure) feeds crafted before/after run
// records through the full grade -> compare -> render path and asserts the
// dropped rule surfaces as a Regression while the untouched rule stays stable.
// The env-gated test (TestDetection_Live) builds a real degraded Environment —
// a CLAUDE.md with one rule's backing line removed on a branch — and runs
// claude -p against it, proving the model behavior half too. Fixtures live in
// testdata/ so the proof is reproducible.
package detection
