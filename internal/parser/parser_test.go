package parser

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

func TestParse_Flat(t *testing.T) {
	f, err := os.Open("testdata/run-flat-01.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	got, err := Parse(f)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	wantBytes, err := os.ReadFile("testdata/run-flat-01.want.json")
	if err != nil {
		t.Fatal(err)
	}
	var want RunRecord
	if err := json.Unmarshal(wantBytes, &want); err != nil {
		t.Fatalf("unmarshal want: %v", err)
	}

	if !reflect.DeepEqual(*got, want) {
		gotJSON, _ := json.MarshalIndent(got, "", "  ")
		wantJSON, _ := json.MarshalIndent(want, "", "  ")
		t.Errorf("RunRecord mismatch\n--- got ---\n%s\n--- want ---\n%s", gotJSON, wantJSON)
	}
}

func TestParse_Tools(t *testing.T) {
	f, err := os.Open("testdata/run-tools-01.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	got, err := Parse(f)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	wantBytes, err := os.ReadFile("testdata/run-tools-01.want.json")
	if err != nil {
		t.Fatal(err)
	}
	var want RunRecord
	if err := json.Unmarshal(wantBytes, &want); err != nil {
		t.Fatalf("unmarshal want: %v", err)
	}

	if !reflect.DeepEqual(*got, want) {
		gotJSON, _ := json.MarshalIndent(got, "", "  ")
		wantJSON, _ := json.MarshalIndent(want, "", "  ")
		t.Errorf("RunRecord mismatch\n--- got ---\n%s\n--- want ---\n%s", gotJSON, wantJSON)
	}

	// Sub-agent internal tool_use (parent_tool_use_id set) is excluded; only the
	// three top-level calls are counted.
	if len(got.ToolCalls) != 3 {
		t.Errorf("tool calls = %d, want 3 (top-level only)", len(got.ToolCalls))
	}

	// Token totals come from modelUsage (true session aggregate), not result.usage
	// (final turn only): input = (12000+4000+80000)+500, output = 1500+50.
	if got.TotalInputTokens() != 96500 {
		t.Errorf("TotalInputTokens = %d, want 96500", got.TotalInputTokens())
	}
	if got.TotalOutputTokens() != 1550 {
		t.Errorf("TotalOutputTokens = %d, want 1550", got.TotalOutputTokens())
	}
}
