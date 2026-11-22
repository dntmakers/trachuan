package evaluation

import (
	"fmt"
	"strings"
	"testing"
)

func warningKnowledge() []byte {
	h := func(c string) string { return "sha256:" + strings.Repeat(c, 64) }
	depends := fmt.Sprintf("{number_norm: '14/2020/nd-cp', text_source: source_tree, text_sha256: '%s', heading_sha256: '%s', ancestors: []}", h("a"), h("b"))
	return []byte(fmt.Sprintf(`- id: effect-example
  kind: provision_effect
  target: {key: 'vn:vbpl:1', anchor: dieu-5}
  claim: {acting: {key: 'vn:vbpl:2', anchor: 'dieu-30/khoan-2'}, relation_kind: repeals}
  evidence: {quote: 'exact text of the acting provision'}
  depends: {contract: 1, acting: %s, target: %s}
  reviewed: {date: '2099-11-18', by: owner, method: human}
`, depends, depends))
}

func TestBindWarningCases_OutcomesAndRefusals(t *testing.T) {
	cases := []WarningCase{{EntryID: "effect-example", Target: "vn:vbpl:1#dieu-5", Acting: "vn:vbpl:2#dieu-30/khoan-2", RelationKind: "repeals"}}
	for _, tc := range []struct {
		name              string
		knowledge, report []byte
		outcome, reason   string
		fail              bool
	}{
		{"absent files", nil, nil, "missing", "", false},
		{"applied", warningKnowledge(), []byte(`[{"id":"effect-example","kind":"provision_effect","target_key":"vn:vbpl:1","outcome":"applied"}]`), "applied", "", false},
		{"redundant", warningKnowledge(), []byte(`[{"id":"effect-example","kind":"provision_effect","target_key":"vn:vbpl:1","outcome":"redundant"}]`), "redundant", "", false},
		{"stale", warningKnowledge(), []byte(`[{"id":"effect-example","kind":"provision_effect","target_key":"vn:vbpl:1","outcome":"stale","reason":"text"}]`), "stale", "text", false},
		{"unpaired", warningKnowledge(), nil, "", "", true},
		{"missing row", warningKnowledge(), []byte(`[]`), "", "", true},
		{"wrong target", warningKnowledge(), []byte(`[{"id":"effect-example","kind":"provision_effect","target_key":"vn:vbpl:9","outcome":"applied"}]`), "", "", true},
		{"duplicate field", warningKnowledge(), []byte(`[{"id":"effect-example","id":"effect-example","kind":"provision_effect","target_key":"vn:vbpl:1","outcome":"applied"}]`), "", "", true},
		{"unknown outcome", warningKnowledge(), []byte(`[{"id":"effect-example","kind":"provision_effect","target_key":"vn:vbpl:1","outcome":"missing"}]`), "", "", true},
		{"contradictory entry", []byte(strings.Replace(string(warningKnowledge()), "dieu-5", "dieu-6", 1)), []byte(`[{"id":"effect-example","kind":"provision_effect","target_key":"vn:vbpl:1","outcome":"applied"}]`), "", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := BindWarningCases(cases, "vn", tc.knowledge, tc.report)
			if tc.fail {
				if err == nil {
					t.Fatal("binding accepted invalid data")
				}
				return
			}
			if err != nil || len(got) != 1 || got[0].Outcome != tc.outcome || got[0].Reason != tc.reason || len(got[0].Search) != 0 || got[0].Read != nil {
				t.Fatalf("binding = %+v, %v", got, err)
			}
		})
	}
}
