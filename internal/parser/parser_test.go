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
