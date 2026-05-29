package judge

import "context"

var _ Judge = StubJudge{}

// StubJudge is a canned Judge for deterministic tests across packages (dsl,
// runner, report). It never calls an LLM. Set ScoreFunc/PreferFunc for
// per-input behavior, or the static fields for a fixed response.
type StubJudge struct {
	ScoreValue int
	ScoreErr   error
	Pref       Preference
	PrefErr    error
	ScoreFunc  func(ctx context.Context, question, answer string, rubric []string) (int, error)
	PreferFunc func(ctx context.Context, question, before, after string) (Preference, error)
}

func (s StubJudge) Score(ctx context.Context, question, answer string, rubric []string) (int, error) {
	if s.ScoreFunc != nil {
		return s.ScoreFunc(ctx, question, answer, rubric)
	}
	return s.ScoreValue, s.ScoreErr
}

func (s StubJudge) Prefer(ctx context.Context, question, before, after string) (Preference, error) {
	if s.PreferFunc != nil {
		return s.PreferFunc(ctx, question, before, after)
	}
	return s.Pref, s.PrefErr
}
