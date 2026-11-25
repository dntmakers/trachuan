package webapi

import (
	"bytes"
	"testing"

	"danny.vn/trachuan/internal/retrieval"
)

func TestWeb_TimelineRejectsDuplicateFields(t *testing.T) {
	h := newHarness(t)
	args := `{"country":"vn","items":[{"handle":"vn:1","timeline":null,"timeline":true}]}`
	web := webCall(h, "POST", "/api/v1/read", args)
	if web.Code != 400 || !bytes.Contains(web.Body.Bytes(), []byte(`"invalid_request"`)) {
		t.Fatalf("duplicate field accepted: %d %s", web.Code, web.Body.String())
	}
}

func TestWeb_TimelineEqualsMCP(t *testing.T) {
	h := newHarness(t)
	h.engine.readResult = &retrieval.ReadResult{Release: "20991110_1", Nodes: map[string]retrieval.NodeRef{}, Items: []retrieval.ItemResult{{Doc: "vn:1", Complete: true, Units: []retrieval.Unit{{Kind: "timeline", Event: &retrieval.TimelineEvent{Kind: "repeals", Actor: &retrieval.TimelineActor{Key: "vn:2", InRelease: true}, Target: &retrieval.TimelineTarget{Scope: "document"}}}}}}}
	args := `{"country":"vn","release":"20991110_1","items":[{"handle":"vn:1","timeline":true}]}`
	web := webCall(h, "POST", "/api/v1/read", args)
	mcp := mcpCall(t, h, "read", args)
	if web.Code != 200 || !bytes.Equal(web.Body.Bytes(), mcp) {
		t.Fatalf("different timeline response %d: %s / %s", web.Code, web.Body.String(), mcp)
	}
	if len(h.engine.read.Items) != 1 || h.engine.read.Items[0].Timeline == nil || !*h.engine.read.Items[0].Timeline {
		t.Fatalf("timeline not forwarded: %+v", h.engine.read)
	}
}
