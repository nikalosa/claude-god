package cli

import (
	"fmt"
	"sort"
	"strings"

	"github.com/nikalosa/claude-god/internal/dsl"
)

const allKinds = "rule_based,open_ended,plan"

func parseKinds(s string) (map[dsl.ProbeKind]bool, error) {
	set := map[dsl.ProbeKind]bool{}
	for _, tok := range strings.Split(s, ",") {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			continue
		}
		switch dsl.ProbeKind(tok) {
		case dsl.RuleBased, dsl.OpenEnded, dsl.Plan:
			set[dsl.ProbeKind(tok)] = true
		default:
			return nil, fmt.Errorf("unknown kind %q (want rule_based, open_ended, plan)", tok)
		}
	}
	if len(set) == 0 {
		return nil, fmt.Errorf("--kind is empty")
	}
	return set, nil
}

func filterByKind(probes []dsl.Probe, set map[dsl.ProbeKind]bool) ([]dsl.Probe, error) {
	out := make([]dsl.Probe, 0, len(probes))
	for _, p := range probes {
		if set[p.Kind] {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		kinds := make([]string, 0, len(set))
		for k := range set {
			kinds = append(kinds, string(k))
		}
		sort.Strings(kinds)
		return nil, fmt.Errorf("no probes of kind %s in corpus", strings.Join(kinds, ", "))
	}
	return out, nil
}
