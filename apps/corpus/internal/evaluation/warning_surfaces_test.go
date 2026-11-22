package evaluation

import (
	"context"
	"testing"

	"danny.vn/trachuan/internal/retrieval"
)

type warningPageReader struct {
	pages    []retrieval.ItemResult
	requests []retrieval.ReadRequest
}

func (r *warningPageReader) Read(_ context.Context, req retrieval.ReadRequest) (retrieval.ReadResult, error) {
	r.requests = append(r.requests, req)
	return retrieval.ReadResult{Release: req.Release, Items: []retrieval.ItemResult{r.pages[len(r.requests)-1]}}, nil
}

func TestWarningRead_PagingExactUnit(t *testing.T) {
	c := WarningCase{EntryID: "effect-example", Target: "vn:vbpl:1#dieu-5", Acting: "vn:vbpl:2#dieu-30", RelationKind: "repeals"}
	w := retrieval.Warning{Pointer: retrieval.Pointer{Dir: "in", Kind: c.RelationKind, Key: "vn:vbpl:2", InRelease: true}, Source: c.Acting, Target: c.Target, Exact: true}
	reader := &warningPageReader{pages: []retrieval.ItemResult{{Doc: "vn:vbpl:1", Units: []retrieval.Unit{{Kind: "provision", Anchor: "chuong-1", Warnings: []retrieval.Warning{w}}}, Next: "next"}, {Doc: "vn:vbpl:1", Units: []retrieval.Unit{{Kind: "provision", Anchor: "dieu-5", Part: 1, Parts: 2, Warnings: []retrieval.Warning{w}}}}}}
	match, err := readWarning(context.Background(), reader, "20990101", c)
	if err != nil || match == nil || match.Carrier != c.Target || len(reader.requests) != 2 || reader.requests[1].Items[0].Next != "next" {
		t.Fatalf("match = %+v, requests %v, error %v", match, reader.requests, err)
	}
	reader = &warningPageReader{pages: []retrieval.ItemResult{{Doc: "vn:vbpl:1", Units: []retrieval.Unit{{Kind: "provision", Anchor: "chuong-1", Warnings: []retrieval.Warning{w}}}}}}
	match, err = readWarning(context.Background(), reader, "20990101", c)
	if err != nil || match != nil {
		t.Fatalf("ancestor alone matched: %+v, %v", match, err)
	}
}

func TestWarningRead_FirstTargetPartIsAuthoritative(t *testing.T) {
	c := WarningCase{EntryID: "effect-example", Target: "vn:vbpl:1#dieu-5", Acting: "vn:vbpl:2#dieu-30", RelationKind: "repeals"}
	w := retrieval.Warning{Pointer: retrieval.Pointer{Dir: "in", Kind: c.RelationKind, Key: "vn:vbpl:2", InRelease: true}, Source: c.Acting, Target: c.Target, Exact: true}
	reader := &warningPageReader{pages: []retrieval.ItemResult{
		{Doc: "vn:vbpl:1", Units: []retrieval.Unit{{Kind: "provision", Anchor: "dieu-5", Part: 1, Parts: 2}}, Next: "next"},
		{Doc: "vn:vbpl:1", Units: []retrieval.Unit{{Kind: "provision", Anchor: "dieu-5", Part: 2, Parts: 2, Warnings: []retrieval.Warning{w}}}},
	}}
	match, err := readWarning(context.Background(), reader, "20990101", c)
	if err != nil || match != nil || len(reader.requests) != 1 {
		t.Fatalf("match = %+v, pages = %d, error = %v", match, len(reader.requests), err)
	}
}

func TestWarningSearch_PresentedScopeAndIdentity(t *testing.T) {
	c := WarningCase{EntryID: "effect-example", Target: "vn:vbpl:1#dieu-5", Acting: "vn:vbpl:2#dieu-30", RelationKind: "repeals"}
	w := retrieval.Warning{Pointer: retrieval.Pointer{Dir: "in", Kind: c.RelationKind, Key: "vn:vbpl:2", InRelease: true}, Source: c.Acting, Target: c.Target, Exact: true}
	scope := func(_ context.Context, _, carrier string) (bool, error) { return carrier != "vn:vbpl:1#dieu-6", nil }
	for _, tc := range []struct {
		name, carrier string
		warnings      []retrieval.Warning
		match         bool
	}{
		{"ancestor", "vn:vbpl:1#chuong-1", []retrieval.Warning{w}, true},
		{"descendant", "vn:vbpl:1#dieu-5/khoan-1", []retrieval.Warning{w}, true},
		{"sibling", "vn:vbpl:1#dieu-6", []retrieval.Warning{w}, false},
		{"capped", c.Target, nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := retrieval.SearchResult{NonCurrent: []retrieval.PresentedHit{{Handle: tc.carrier, Warnings: tc.warnings}}}
			got, err := matchWarningSearch(context.Background(), c, result, scope)
			if err != nil || (got != nil) != tc.match {
				t.Fatalf("match = %+v, %v", got, err)
			}
		})
	}
	response := retrieval.SearchResult{Hits: []retrieval.PresentedHit{{Handle: "vn:vbpl:1#chuong-1", Warnings: []retrieval.Warning{w}}}, NonCurrent: []retrieval.PresentedHit{{Handle: c.Target, Warnings: []retrieval.Warning{w}}}}
	first, err := matchWarningSearch(context.Background(), c, response, scope)
	if err != nil || first == nil || first.Carrier != "vn:vbpl:1#chuong-1" {
		t.Fatalf("response order match = %+v, %v", first, err)
	}
	for _, mutate := range []func(*retrieval.Warning){
		func(w *retrieval.Warning) { w.Exact = false },
		func(w *retrieval.Warning) { w.Dir = "out" },
		func(w *retrieval.Warning) { w.Kind = "suspends" },
		func(w *retrieval.Warning) { w.Key = "vn:other" },
		func(w *retrieval.Warning) { w.InRelease = false },
		func(w *retrieval.Warning) { w.Source = "vn:other#dieu-1" },
		func(w *retrieval.Warning) { w.Target = "vn:1#dieu-6" },
	} {
		wrong := w
		mutate(&wrong)
		got, err := matchWarningSearch(context.Background(), c, retrieval.SearchResult{Hits: []retrieval.PresentedHit{{Handle: c.Target, Warnings: []retrieval.Warning{wrong}}}}, scope)
		if err != nil || got != nil {
			t.Fatalf("incorrect identity matched: %+v, %v", got, err)
		}
	}
}
