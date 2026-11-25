package mcpapi

import (
	"encoding/json"
	"testing"

	"danny.vn/trachuan/internal/retrieval"
)

func TestReadTimeline_PublishedSchema(t *testing.T) {
	h := newHarness(t, nil)
	tools, err := h.session.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range tools.Tools {
		if tool.Name != "read" {
			continue
		}
		raw, err := json.Marshal(tool.InputSchema)
		if err != nil {
			t.Fatal(err)
		}
		var schema map[string]any
		if err := json.Unmarshal(raw, &schema); err != nil {
			t.Fatal(err)
		}
		props := schema["properties"].(map[string]any)
		items := props["items"].(map[string]any)["items"].(map[string]any)
		timeline := items["properties"].(map[string]any)["timeline"].(map[string]any)
		if timeline["type"] != "boolean" {
			t.Fatalf("timeline schema = %s", raw)
		}
		return
	}
	t.Fatal("read tool missing")
}

func TestReadTimeline_RejectsDuplicateFields(t *testing.T) {
	h := newHarness(t, nil)
	wantError(t, callTool(t, h, "read", json.RawMessage(`{"country":"vn","items":[{"handle":"vn:1","timeline":null,"timeline":true}]}`)), "invalid_request")
}

func TestReadTimeline_Wire(t *testing.T) {
	h := newHarness(t, nil)
	h.engine.readResult = &retrieval.ReadResult{Release: "20991111", Nodes: map[string]retrieval.NodeRef{}, Items: []retrieval.ItemResult{{Doc: "vn:1", Complete: true, Units: []retrieval.Unit{{Kind: "timeline", Event: &retrieval.TimelineEvent{Kind: "repeals", Date: &retrieval.Date{Year: 2020, Month: 1, Day: 1}, Actor: &retrieval.TimelineActor{Key: "vn:2", InRelease: true}, Target: &retrieval.TimelineTarget{Scope: "document"}}}}}}}
	result := callTool(t, h, "read", map[string]any{"country": "vn", "items": []any{map[string]any{"handle": "vn:1", "timeline": true}}})
	if result.IsError {
		t.Fatalf("read failed: %+v", resultJSON(t, result))
	}
	out := resultJSON(t, result)
	if len(h.engine.read.Items) != 1 || h.engine.read.Items[0].Timeline == nil || !*h.engine.read.Items[0].Timeline || out["items"] == nil {
		t.Fatalf("timeline read = %+v, result = %+v", h.engine.read, out)
	}
}
