package judge

import (
	"context"
	"fmt"
	"testing"
)

// stubBackend returns canned Ask responses in call order, recording prompts.
type stubBackend struct {
	responses []string
	calls     int
	prompts   []string
}

func (s *stubBackend) Ask(_ context.Context, prompt string) (string, error) {
	s.prompts = append(s.prompts, prompt)
	i := s.calls
	s.calls++
	if i >= len(s.responses) {
		return "", fmt.Errorf("stubBackend: no response for call %d", i)
	}
	return s.responses[i], nil
}

func TestExtractJSON(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{"bare", `{"winner":1}`, `{"winner":1}`, false},
		{"fenced", "```json\n{\"winner\":2}\n```", `{"winner":2}`, false},
		{"prose", `Sure, here you go: {"winner":0} — hope that helps!`, `{"winner":0}`, false},
		{"nested", `{"a":{"b":1},"c":2}`, `{"a":{"b":1},"c":2}`, false},
		{"brace-in-string", `{"reasoning":"use {curly} braces"}`, `{"reasoning":"use {curly} braces"}`, false},
		{"escaped-quote-in-string", `{"reasoning":"say \"hi\" now"}`, `{"reasoning":"say \"hi\" now"}`, false},
		{"two-blobs-takes-first", `{"winner":1} and then {"winner":2}`, `{"winner":1}`, false},
		{"none", `no json here`, "", true},
		{"unbalanced", `{"winner":1`, "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := extractJSON(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Errorf("expected error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("extractJSON: %v", err)
			}
			if string(got) != tc.want {
				t.Errorf("got %q want %q", got, tc.want)
			}
		})
	}
}

func factsJSON(present ...bool) string {
	out := `{"facts":[`
	for i, p := range present {
		if i > 0 {
			out += ","
		}
		out += fmt.Sprintf(`{"index":%d,"present":%t}`, i+1, p)
	}
	return out + `]}`
}

func TestScore_Math(t *testing.T) {
	cases := []struct {
		name    string
		present []bool
		rubric  []string
		want    int
	}{
		{"all-present", []bool{true, true}, []string{"a", "b"}, 100},
		{"half", []bool{true, false}, []string{"a", "b"}, 50},
		{"none", []bool{false, false}, []string{"a", "b"}, 0},
		{"one-of-three-rounds-down", []bool{true, false, false}, []string{"a", "b", "c"}, 33},
		{"two-of-three-rounds-up", []bool{true, true, false}, []string{"a", "b", "c"}, 67},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			j := New(&stubBackend{responses: []string{factsJSON(tc.present...)}})
			got, err := j.Score(context.Background(), "q", "a", tc.rubric)
			if err != nil {
				t.Fatalf("Score: %v", err)
			}
			if got != tc.want {
				t.Errorf("score = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestScore_ExtractsFromProse(t *testing.T) {
	resp := "Here is my assessment:\n```json\n" + factsJSON(true) + "\n```\nDone."
	j := New(&stubBackend{responses: []string{resp}})
	got, err := j.Score(context.Background(), "q", "a", []string{"only-fact"})
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if got != 100 {
		t.Errorf("score = %d, want 100", got)
	}
}

func TestScore_EmptyRubric(t *testing.T) {
	j := New(&stubBackend{})
	if _, err := j.Score(context.Background(), "q", "a", nil); err == nil {
		t.Error("expected error for empty rubric")
	}
}

// prefJSON builds an ordering verdict where pos1 vs pos2 winner is `winner`
// (1 = pos1, 2 = pos2, 0 = tie); dimensions mirror the overall winner here.
func prefJSON(winner int) string {
	return fmt.Sprintf(`{"winner":%d,"concise":%d,"exhaustive":%d,"direct":%d,"reasoning":"x"}`,
		winner, winner, winner, winner)
}

func TestPrefer_BothOrderingsAgree_BeforeWins(t *testing.T) {
	// Ordering A: pos1=before,pos2=after; winner 1 -> before.
	// Ordering B: pos1=after,pos2=before; winner 2 -> before.
	j := New(&stubBackend{responses: []string{prefJSON(1), prefJSON(2)}})
	p, err := j.Prefer(context.Background(), "q", "BEFORE", "AFTER")
	if err != nil {
		t.Fatalf("Prefer: %v", err)
	}
	if p.Outcome != BeforeBetter {
		t.Errorf("outcome = %v, want before_better", p.Outcome)
	}
}

func TestPrefer_OrderDependent_CollapsesToTie(t *testing.T) {
	// Ordering A: winner 1 -> before. Ordering B: winner 1 -> after (pos1=after).
	// The two orderings disagree (positional bias) -> tie.
	j := New(&stubBackend{responses: []string{prefJSON(1), prefJSON(1)}})
	p, err := j.Prefer(context.Background(), "q", "BEFORE", "AFTER")
	if err != nil {
		t.Fatalf("Prefer: %v", err)
	}
	if p.Outcome != Tie {
		t.Errorf("outcome = %v, want tie (order-dependent win must collapse)", p.Outcome)
	}
}

func TestPrefer_TieInOneOrdering_IsTie(t *testing.T) {
	// A: tie; B: winner 2 -> before. A side must win BOTH orderings, so -> tie.
	j := New(&stubBackend{responses: []string{prefJSON(0), prefJSON(2)}})
	p, err := j.Prefer(context.Background(), "q", "BEFORE", "AFTER")
	if err != nil {
		t.Fatalf("Prefer: %v", err)
	}
	if p.Outcome != Tie {
		t.Errorf("outcome = %v, want tie", p.Outcome)
	}
}

func TestPrefer_AfterWins(t *testing.T) {
	// A: winner 2 -> after. B: winner 1 -> after (pos1=after).
	j := New(&stubBackend{responses: []string{prefJSON(2), prefJSON(1)}})
	p, err := j.Prefer(context.Background(), "q", "BEFORE", "AFTER")
	if err != nil {
		t.Fatalf("Prefer: %v", err)
	}
	if p.Outcome != AfterBetter {
		t.Errorf("outcome = %v, want after_better", p.Outcome)
	}
}

func TestPrefer_RunsBothOrderings(t *testing.T) {
	sb := &stubBackend{responses: []string{prefJSON(0), prefJSON(0)}}
	j := New(sb)
	if _, err := j.Prefer(context.Background(), "q", "BEFORE", "AFTER"); err != nil {
		t.Fatalf("Prefer: %v", err)
	}
	if sb.calls != 2 {
		t.Errorf("expected 2 backend calls (both orderings), got %d", sb.calls)
	}
}
