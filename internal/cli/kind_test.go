package cli

import (
	"testing"

	"github.com/nikalosa/claude-god/internal/dsl"
)

func TestParseKinds(t *testing.T) {
	cases := []struct {
		in      string
		want    []dsl.ProbeKind
		wantErr bool
	}{
		{"rule_based", []dsl.ProbeKind{dsl.RuleBased}, false},
		{"open_ended,plan", []dsl.ProbeKind{dsl.OpenEnded, dsl.Plan}, false},
		{" rule_based , plan ", []dsl.ProbeKind{dsl.RuleBased, dsl.Plan}, false},
		{"plan,plan", []dsl.ProbeKind{dsl.Plan}, false},
		{allKinds, []dsl.ProbeKind{dsl.RuleBased, dsl.OpenEnded, dsl.Plan}, false},
		{"bogus", nil, true},
		{"rule_based,bogus", nil, true},
		{"", nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			set, err := parseKinds(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Errorf("parseKinds(%q): expected error", tc.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseKinds(%q): %v", tc.in, err)
			}
			for _, w := range tc.want {
				if !set[w] {
					t.Errorf("parseKinds(%q) missing %q", tc.in, w)
				}
			}
			if len(set) != len(tc.want) {
				t.Errorf("parseKinds(%q) = %v, want exactly %v", tc.in, set, tc.want)
			}
		})
	}
}

func TestFilterByKind(t *testing.T) {
	probes := []dsl.Probe{
		{ID: "r1", Kind: dsl.RuleBased},
		{ID: "o1", Kind: dsl.OpenEnded},
		{ID: "p1", Kind: dsl.Plan},
		{ID: "r2", Kind: dsl.RuleBased},
	}

	t.Run("subset keeps only selected kind and preserves order", func(t *testing.T) {
		got, err := filterByKind(probes, map[dsl.ProbeKind]bool{dsl.RuleBased: true})
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 2 || got[0].ID != "r1" || got[1].ID != "r2" {
			t.Errorf("want [r1 r2], got %+v", got)
		}
	})

	t.Run("all kinds keep everything", func(t *testing.T) {
		got, err := filterByKind(probes, map[dsl.ProbeKind]bool{dsl.RuleBased: true, dsl.OpenEnded: true, dsl.Plan: true})
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != len(probes) {
			t.Errorf("want all %d, got %d", len(probes), len(got))
		}
	})

	t.Run("kind absent from corpus errors", func(t *testing.T) {
		only := []dsl.Probe{{ID: "r1", Kind: dsl.RuleBased}}
		if _, err := filterByKind(only, map[dsl.ProbeKind]bool{dsl.Plan: true}); err == nil {
			t.Error("expected error when no probe matches the selected kind")
		}
	})
}
