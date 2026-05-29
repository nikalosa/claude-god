// Package judge is the LLM-based grader, isolated from the deterministic
// pattern path so its run-to-run noise never touches regex grading.
//
// It runs in two modes: Score (absolute rubric scoring for rule-based probes a
// regex cannot express) and Prefer (comparative preference for open-ended
// probes — report-only). The backend invokes `claude -p` in a throwaway empty
// working directory so no target Environment can sway it, reusing the
// developer's OAuth login with no API key (ADR-0003). The Backend interface
// hides the backend so a later swap to the Anthropic API is a localized change.
//
// Judge methods take and return only plain strings/values — never dsl or parser
// types — so judge stays an import leaf and dsl->judge cannot cycle.
package judge
