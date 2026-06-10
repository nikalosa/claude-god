package judge

import (
	"context"
	"os"
	"testing"
)

func TestParseEnvelope_Success(t *testing.T) {
	data, err := os.ReadFile("testdata/envelope-success-01.json")
	if err != nil {
		t.Fatal(err)
	}
	got, err := parseEnvelope(data)
	if err != nil {
		t.Fatalf("parseEnvelope: %v", err)
	}
	want := `{"facts":[{"index":1,"present":true},{"index":2,"present":false}]}`
	if got != want {
		t.Errorf("result mismatch\n got: %q\nwant: %q", got, want)
	}
}

func TestParseEnvelope_ErrorEnvelope(t *testing.T) {
	data, err := os.ReadFile("testdata/envelope-error-01.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parseEnvelope(data); err == nil {
		t.Error("expected error for is_error=true envelope")
	}
}

func TestParseEnvelope_Malformed(t *testing.T) {
	if _, err := parseEnvelope([]byte("this is not json")); err == nil {
		t.Error("expected error for non-JSON input")
	}
}

func TestParseEnvelope_EmptyResult(t *testing.T) {
	env := []byte(`{"type":"result","subtype":"success","is_error":false,"result":""}`)
	if _, err := parseEnvelope(env); err == nil {
		t.Error("expected error for empty result")
	}
}

// TestClaudeBackend_Smoke exercises the live claude -p judge path. Gated behind
// CLAUDE_BENCHMARK_DOGFOOD=1 because it shells out to a real invocation (costs
// money, takes seconds) — the parsing paths above are covered without it.
func TestClaudeBackend_Smoke(t *testing.T) {
	if os.Getenv("CLAUDE_BENCHMARK_DOGFOOD") != "1" {
		t.Skip("set CLAUDE_BENCHMARK_DOGFOOD=1 to run")
	}
	j := New(NewClaudeBackend())
	score, err := j.Score(context.Background(),
		"What color is the sky on a clear day?",
		"The sky is blue on a clear day.",
		[]string{"the sky is blue", "this is about elephants"})
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if score < 0 || score > 100 {
		t.Errorf("score out of range: %d", score)
	}
	t.Logf("live judge score: %d (expected ~50: one fact present, one absent)", score)
}
