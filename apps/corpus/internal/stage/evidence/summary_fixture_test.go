package evidence

import (
	"context"
	"slices"
	"strings"

	"danny.vn/trachuan/apps/corpus/internal/stage/membership"
	"danny.vn/trachuan/apps/corpus/internal/stage/releasechange"
)

func sortRowsByAnchor(rows []summaryProvision) {
	slices.SortFunc(rows, func(a, b summaryProvision) int { return strings.Compare(a.Anchor, b.Anchor) })
}

type fakeSummary struct {
	documents  []summaryDocument
	nodes      map[int64]taxonomyNode
	provisions map[string][]summaryProvision
	relations  []summaryRelation
	err        error
}

func (f *fakeSummary) SummaryDocuments(context.Context) ([]summaryDocument, error) {
	docs := slices.Clone(f.documents)
	slices.SortFunc(docs, func(a, b summaryDocument) int { return strings.Compare(a.Key, b.Key) })
	return docs, f.err
}

func (f *fakeSummary) SummaryNodes(context.Context) (map[int64]taxonomyNode, error) {
	return f.nodes, f.err
}

func (f *fakeSummary) SummaryProvisions(_ context.Context, fn func(string, []summaryProvision) error) error {
	keys := make([]string, 0, len(f.provisions))
	for key := range f.provisions {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		rows := slices.Clone(f.provisions[key])
		sortRowsByAnchor(rows)
		if err := fn(key, rows); err != nil {
			return err
		}
	}
	return f.err
}

func (f *fakeSummary) SummaryRelations(_ context.Context, fn func(summaryRelation) error) error {
	for _, r := range f.relations {
		if err := fn(r); err != nil {
			return err
		}
	}
	return f.err
}

func doc(key, number, status string) summaryDocument {
	return summaryDocument{Key: key, NumberNorm: number, StatusClass: status, EffFrom: ptr("2099-01-01"), IsCurrent: status == "in_force" || status == "partial", TextSource: "source_tree", TextComplete: true}
}

func textHash(text string) string { return hashPrefix + sha(text) }

func prov(id, parent int64, anchor, text string, assignments ...assignment) summaryProvision {
	p := row(id, parent, anchor, assignments...)
	p.TextSHA256 = textHash(text)
	return p
}

func rel(kind, method, source, sourceNumber string, sourceAnchor *string, target, targetNumber string, targetIn bool, targetAnchor, node *string) summaryRelation {
	r := summaryRelation{Kind: kind, Method: method, SourceKey: source, SourceNumber: sourceNumber, SourceInRelease: true, SourceAnchor: sourceAnchor, TargetNumber: targetNumber, TargetInRelease: targetIn, TargetAnchor: targetAnchor, TargetNode: node}
	if target != "" {
		r.TargetKey = &target
	}
	return r
}

func testNodes() map[int64]taxonomyNode {
	return map[int64]taxonomyNode{11: {Code: "1.1.a", Served: true}, 12: {Code: "1.1.b", Served: true}, 21: {Code: "2.1.a", Served: true}}
}

func seed(node, family int64) assignment {
	return assignment{Node: node, Family: family, Method: "phapdien_seed"}
}

func activeMembers(keys ...string) members {
	entries := make([]membership.Entry, 0, len(keys))
	for _, key := range keys {
		entries = append(entries, membership.Entry{DocumentKey: key, Disposition: "active"})
	}
	m, err := releasechange.NewMembers(entries, "test", "test")
	if err != nil {
		panic(err)
	}
	return m
}

func withMembers(m members, key, disposition, replacement string) members {
	entries := m.Entries()
	entries = slices.DeleteFunc(entries, func(e membership.Entry) bool { return e.DocumentKey == key })
	entry := membership.Entry{DocumentKey: key, Disposition: disposition}
	if replacement != "" {
		entry.ReplacementKey = &replacement
	}
	entries = append(entries, entry)
	updated, err := releasechange.NewMembers(entries, "test", "test")
	if err != nil {
		panic(err)
	}
	return updated
}

func previousSummary() (*fakeSummary, members) {
	one := doc("vn:vbpl:2099001", "01/2099/TT-NHNN", "in_force")
	handed := doc("vn:chinhphu:2099003", "3/2099/ND-CP", "in_force")
	handed.TextSource, handed.TextComplete = "pdf_text", false
	src := &fakeSummary{
		documents: []summaryDocument{one, doc("vn:vbpl:2099002", "2/2099/TT-NHNN", "in_force"), handed, doc("vn:vbpl:2099006", "6/2099/QH15", "in_force"), doc("vn:chinhphu:2099007", "7/2099/ND-CP", "in_force")},
		nodes:     testNodes(),
		provisions: map[string][]summaryProvision{
			"vn:vbpl:2099001":     {prov(1, 0, "dieu-1", "Điều 1 cũ", seed(11, 1)), prov(2, 1, "dieu-1/khoan-1", "Khoản 1"), prov(3, 0, "dieu-2", "Điều 2", seed(11, 1)), prov(4, 0, "dieu-4", "Điều 4")},
			"vn:vbpl:2099002":     {prov(10, 0, "dieu-1", "Điều 1 văn bản 2")},
			"vn:chinhphu:2099003": {prov(20, 0, "dieu-1", "Điều 1 nghị định"), prov(21, 0, "dieu-2", "Điều 2 nghị định")},
		},
		relations: []summaryRelation{
			rel("cites", "vbpl_diagram", "vn:vbpl:2099001", "1/2099/TT-NHNN", nil, "vn:chinhphu:2099003", "3/2099/NĐ-CP", true, nil, nil),
			rel("guided_by", "vbpl_diagram", "vn:vbpl:2099001", "1/2099/TT-NHNN", nil, "vn:vbpl:2099002", "2/2099/TT-NHNN", true, nil, nil),
			rel("repeals", "text_rule/1", "vn:vbpl:2099001", "1/2099/TT-NHNN", ptr("dieu-4"), "", "9/2098/TT", false, nil, ptr("dieu-3")),
		},
	}
	m := activeMembers("vn:vbpl:2099001", "vn:vbpl:2099002", "vn:chinhphu:2099003", "vn:vbpl:2099005", "vn:vbpl:2099006", "vn:chinhphu:2099007")
	return src, m
}

func currentSummary() (*fakeSummary, members) {
	one := doc("vn:vbpl:2099001", "1/2099/TT-NHNN", "expired")
	one.EffTo, one.TextSource = ptr("2099-06-01"), "html_transcription"
	replacement := doc("vn:vbpl:2099004", "3/2099/ND-CP", "in_force")
	five := doc("vn:vbpl:2099005", "5/2099/TT-BTC", "in_force")
	five.Baseline = true
	nine := doc("vn:vbpl:2099009", "9/2099/QD-TTG", "not_yet")
	two := prov(3, 0, "dieu-2", "Điều 2", seed(12, 1))
	two.Validity = []validityRow{{StatusClass: "expired", EffFrom: ptr("2099-03-01"), Method: "text"}}
	src := &fakeSummary{
		documents: []summaryDocument{one, replacement, five, nine, doc("vn:vbpl:2099010", "10/2099/ND-CP", "in_force")},
		nodes:     testNodes(),
		provisions: map[string][]summaryProvision{
			"vn:vbpl:2099001": {prov(1, 0, "dieu-1", "Điều 1 mới", seed(11, 1)), two, prov(4, 0, "dieu-4", "Điều 4"), prov(5, 0, "dieu-4~2", "Điều 4 lặp"), prov(6, 0, "dieu-3", "Điều 3")},
			"vn:vbpl:2099004": {prov(20, 0, "dieu-1", "Điều 1 nghị định"), prov(22, 0, "dieu-3", "Điều 3 nghị định")},
		},
		relations: []summaryRelation{
			rel("cites", "vbpl_diagram", "vn:vbpl:2099001", "1/2099/TT-NHNN", nil, "vn:vbpl:2099004", "3/2099/NĐ-CP", true, nil, nil),
			rel("repeals", "text_rule/1", "vn:vbpl:2099001", "1/2099/TT-NHNN", ptr("dieu-4"), "", "9/2098/TT", false, nil, ptr("dieu-3")),
			rel("repeals", "text_rule/1", "vn:vbpl:2099001", "1/2099/TT-NHNN", ptr("dieu-4"), "", "9/2098/TT", false, nil, ptr("dieu-3")),
			rel("amends_supplements", "text_rule/1", "vn:vbpl:2099009", "9/2099/QĐ-TTg", ptr("dieu-1"), "vn:vbpl:2099001", "1/2099/TT-NHNN", true, ptr("dieu-2"), ptr("dieu-2/khoan-3")),
		},
	}
	m := activeMembers("vn:vbpl:2099001", "vn:vbpl:2099004", "vn:vbpl:2099005", "vn:vbpl:2099006", "vn:vbpl:2099009", "vn:vbpl:2099010")
	m = withMembers(m, "vn:vbpl:2099002", "withdrawn", "")
	m = withMembers(m, "vn:chinhphu:2099003", "source_replaced", "vn:vbpl:2099004")
	m = withMembers(m, "vn:chinhphu:2099007", "source_replaced", "vn:vbpl:2099008")
	m = withMembers(m, "vn:chinhphu:2099011", "source_replaced", "vn:vbpl:2099010")
	return src, m
}
