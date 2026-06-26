package parser

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

// TestParse_MCPServers pins that the init-event mcp_servers roster (name+status)
// is captured, so the harness can reject a run where a declared server loaded
// disabled instead of silently grading it as "server present".
func TestParse_MCPServers(t *testing.T) {
	stream := `{"type":"system","subtype":"init","session_id":"s","model":"m","cwd":"/wt","mcp_servers":[{"name":"codegraph","status":"disabled"}]}
{"type":"result","subtype":"success","result":"hi","total_cost_usd":0.01}
`
	got, err := Parse(strings.NewReader(stream))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	want := []MCPServer{{Name: "codegraph", Status: "disabled"}}
	if !reflect.DeepEqual(got.MCPServers, want) {
		t.Errorf("MCPServers = %+v, want %+v", got.MCPServers, want)
	}
}

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

	// Peak resident context is the max over MAIN-session turns (20k, 50k, 45k);
	// the sub-agent turn's 999999 (parent_tool_use_id set) is excluded.
	if got.ContextWindowTokens() != 50000 {
		t.Errorf("ContextWindowTokens = %d, want 50000 (max main turn, sub-agent excluded)", got.ContextWindowTokens())
	}
}
