package cli

import "testing"

func TestParseLevels(t *testing.T) {
	cases := []struct {
		in      string
		want    []string
		wantErr bool
	}{
		{"l1", []string{"l1"}, false},
		{"l2", []string{"l2"}, false},
		{"l1,l2", []string{"l1", "l2"}, false},
		{" l1 , l2 ", []string{"l1", "l2"}, false},
		{"l2,l2", []string{"l2"}, false},
		{"l3", nil, true},
		{"l1,l4", nil, true},
		{"bogus", nil, true},
		{"", nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			set, err := parseLevels(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Errorf("parseLevels(%q): expected error", tc.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseLevels(%q): %v", tc.in, err)
			}
			for _, w := range tc.want {
				if !set[w] {
					t.Errorf("parseLevels(%q) missing %q", tc.in, w)
				}
			}
			if len(set) != len(tc.want) {
				t.Errorf("parseLevels(%q) = %v, want exactly %v", tc.in, set, tc.want)
			}
		})
	}
}

func TestValidateSamples(t *testing.T) {
	for _, n := range []int{1, 3, 5, 7} {
		if err := validateSamples(n); err != nil {
			t.Errorf("validateSamples(%d): unexpected error %v", n, err)
		}
	}
	for _, n := range []int{0, -1, 2, 4} {
		if err := validateSamples(n); err == nil {
			t.Errorf("validateSamples(%d): expected error", n)
		}
	}
}
