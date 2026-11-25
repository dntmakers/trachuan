//go:build integration

package webapi

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"

	"danny.vn/trachuan/internal/retrieval"
)

func TestSearchIssuedContractIntegration(t *testing.T) {
	engine := taxonomyIntegrationEngine(t, `UPDATE document SET issue_date=CASE id WHEN 1 THEN '2024-01-01'::date WHEN 2 THEN '2025-01-01'::date ELSE NULL END`, `UPDATE document SET eff_from='2020-01-01',eff_to='2025-01-01',status_class='expired',is_current=false WHERE id=3`)
	h := newHarness(t)
	h.resolver.engine = engine
	call := func(args map[string]any) retrieval.SearchResult {
		t.Helper()
		raw, err := json.Marshal(args)
		if err != nil {
			t.Fatal(err)
		}
		web := webCall(h, "POST", "/api/v1/search", string(raw))
		mcp := mcpCall(t, h, "search", string(raw))
		if web.Code != 200 || !bytes.Equal(web.Body.Bytes(), mcp) {
			t.Fatalf("web %d %s; MCP %s", web.Code, web.Body.String(), mcp)
		}
		var result retrieval.SearchResult
		if err := json.Unmarshal(mcp, &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	type searchWalk struct {
		groups          [2][]string
		statuses        map[string]retrieval.Status
		abstain         bool
		reason, message string
	}
	walk := func(extra map[string]any, membership map[string]bool) searchWalk {
		t.Helper()
		args := map[string]any{"country": "vn", "query": "ngân hàng", "limit": 1, "in_force_only": false, "as_of": "2099-11-10"}
		for k, v := range extra {
			args[k] = v
		}
		out := searchWalk{statuses: map[string]retrieval.Status{}}
		pages := 0
		for {
			result := call(args)
			if pages == 0 {
				out.abstain, out.reason, out.message = result.Abstain, result.Reason, result.Message
			} else if result.Abstain != out.abstain || result.Reason != out.reason || result.Message != out.message {
				t.Fatal("abstention changed between pages")
			}
			pages++
			if membership != nil {
				outside := 0
				for _, inside := range membership {
					if !inside {
						outside++
					}
				}
				if result.OutsideFilterCount == nil || *result.OutsideFilterCount != outside {
					t.Fatalf("outside count: %+v", result.OutsideFilterCount)
				}
			} else if result.OutsideFilterCount != nil {
				t.Fatal("unfiltered response has count")
			}
			for group, hits := range [][]retrieval.PresentedHit{result.Hits, result.NonCurrent} {
				for _, hit := range hits {
					out.groups[group] = append(out.groups[group], hit.Doc)
					if membership == nil && hit.InFilter != nil || membership != nil && (hit.InFilter == nil || *hit.InFilter != membership[hit.Doc]) {
						t.Fatalf("in_filter: %+v", hit)
					}
					doc, ok := result.Documents[hit.Doc]
					if !ok {
						t.Fatalf("missing document for %s", hit.Doc)
					}
					out.statuses[hit.Doc] = doc.Status
				}
			}
			if result.NextCursor == "" {
				break
			}
			if pages > 3 {
				t.Fatal("cursor did not terminate")
			}
			args["cursor"] = result.NextCursor
		}
		if len(out.groups[0])+len(out.groups[1]) != 3 || pages < 2 {
			t.Fatalf("expected three paged hits: %v", out.groups)
		}
		return out
	}
	baseline := walk(nil, nil)
	if len(baseline.groups[0]) != 2 || len(baseline.groups[1]) != 1 || baseline.groups[1][0] != "vn:3" || baseline.statuses["vn:3"].Class != "expired" {
		t.Fatalf("fixture validity groups: %+v", baseline)
	}
	cases := []struct {
		name    string
		args    map[string]any
		members map[string]bool
	}{
		{"from", map[string]any{"issued_from": "2025-01-01"}, map[string]bool{"vn:1": false, "vn:2": true, "vn:3": false}},
		{"to", map[string]any{"issued_to": "2024-01-01"}, map[string]bool{"vn:1": true, "vn:2": false, "vn:3": false}},
		{"equal", map[string]any{"issued_from": "2024-01-01", "issued_to": "2024-01-01"}, map[string]bool{"vn:1": true, "vn:2": false, "vn:3": false}},
		{"combined", map[string]any{"issued_from": "2024-01-01", "issued_to": "2025-01-01", "nodes": []string{"1.1"}, "doc_types": []string{"law"}, "issuers": []string{"issuer"}, "documents": []string{"vn:1", "vn:2"}, "as_of": "2099-11-10"}, map[string]bool{"vn:1": true, "vn:2": false, "vn:3": false}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := walk(tc.args, tc.members)
			for group, ids := range baseline.groups {
				var want []string
				for _, inside := range []bool{true, false} {
					for _, id := range ids {
						if tc.members[id] == inside {
							want = append(want, id)
						}
					}
				}
				if !reflect.DeepEqual(got.groups[group], want) {
					t.Fatalf("group %d order got %v want %v", group, got.groups[group], want)
				}
			}
			if !reflect.DeepEqual(got.statuses, baseline.statuses) || got.abstain != baseline.abstain || got.reason != baseline.reason || got.message != baseline.message {
				t.Fatalf("filter changed validity or abstention: got %+v baseline %+v", got, baseline)
			}
		})
	}
	call(map[string]any{"country": "vn", "query": "ngân hàng", "issued_from": "2024-02-29", "issued_to": "2024-02-29"})
	for _, fields := range []string{`"issued_from":null`, `"issued_to":""`, `"issued_from":"2024-02-30"`, `"issued_to":"2024-2-01"`, `"issued_from":"2025-01-01","issued_to":"2024-01-01"`, `"issued_from":"2023-02-29"`, `"issued_to":"0000-01-01"`, `"issued_from":"2024-01-011"`, `"issued_to":"２０２４-０１-０１"`, `"issued_from":"2024-01-01Z"`} {
		args := `{"country":"vn","query":"ngân hàng",` + fields + `}`
		web := webCall(h, "POST", "/api/v1/search", args)
		mcp := mcpCall(t, h, "search", args)
		if web.Code != 400 || !bytes.Contains(web.Body.Bytes(), []byte(`"invalid_request"`)) || !bytes.Contains(mcp, []byte(`"invalid_request"`)) {
			t.Fatalf("invalid %s: web %d %s MCP %s", fields, web.Code, web.Body.String(), mcp)
		}
	}
	for _, bound := range []string{"issued_from", "issued_to"} {
		args := map[string]any{"country": "vn", "query": "ngân hàng", "limit": 1, bound: "2024-01-01"}
		first := call(args)
		if first.NextCursor == "" {
			t.Fatal("missing cursor")
		}
		args["cursor"] = first.NextCursor
		args[bound] = "2025-01-01"
		raw, err := json.Marshal(args)
		if err != nil {
			t.Fatal(err)
		}
		web := webCall(h, "POST", "/api/v1/search", string(raw))
		mcp := mcpCall(t, h, "search", string(raw))
		if web.Code != 400 || !bytes.Contains(web.Body.Bytes(), []byte(`"invalid_request"`)) || !bytes.Contains(mcp, []byte(`"invalid_request"`)) {
			t.Fatalf("changed %s: web %d %s MCP %s", bound, web.Code, web.Body.String(), mcp)
		}
	}
}
