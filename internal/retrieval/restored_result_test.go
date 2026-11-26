package retrieval

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"danny.vn/trachuan/internal/embedding"
)

func TestLegacyRecipesRejectWithoutEmbedding(t *testing.T) {
	for _, recipe := range []Recipe{Recipe2(), Recipe3()} {
		threshold := 0.5
		recipe.Abstention.Threshold = &threshold
		embedder := &fakeEmbedder{}
		e := &Engine{release: Release{Rel: "test", RetrievalRecipe: recipe}, embedder: embedder, queryRecipe: embedding.QueryRecipe1(), options: Options{Now: time.Now}}
		for _, req := range []Request{{Query: "ngan hang"}, {Query: "ngan hang", Documents: []string{"vn:3"}}, {Query: "ngan hang", DocTypes: []string{"law"}}} {
			for _, continuation := range []bool{false, true} {
				if continuation {
					_, cursor, err := e.searchRequest(req)
					if err != nil {
						t.Fatal(err)
					}
					req.Cursor = encodeSearchCursor(cursor)
				}
				result, err := e.Search(context.Background(), req)
				if err != nil || !result.Abstain || result.Reason != "diacritics_required" || result.Message != "Vui lòng nhập câu hỏi tiếng Việt có dấu hoặc số hiệu văn bản." || embedder.calls != 0 {
					t.Fatalf("recipe %d: %+v %v, embeds %d", recipe.Version, result, err, embedder.calls)
				}
			}
		}
	}
}

func TestPresentRestoredQuery(t *testing.T) {
	var response Response
	if err := json.Unmarshal([]byte(`{"restored_query":"ngân hàng","abstain":true}`), &response); err != nil {
		t.Fatal(err)
	}
	for _, detail := range []Detail{DetailCompact, DetailFull} {
		unchanged, err := json.Marshal(present(Response{}, nil, detail))
		if err != nil || strings.Contains(string(unchanged), `"restored_query"`) {
			t.Fatalf("unchanged: %s %v", unchanged, err)
		}
		raw, err := json.Marshal(present(response, nil, detail))
		if err != nil || !strings.Contains(string(raw), `"restored_query":"ngân hàng"`) {
			t.Fatalf("%s: %s %v", detail, raw, err)
		}
	}
}
