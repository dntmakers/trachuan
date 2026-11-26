package mcpapi

import (
	"context"
	"testing"
	"time"

	"danny.vn/trachuan/internal/retrieval"
)

type gateEngine struct {
	*fakeEngine
	gate string
}

func (e *gateEngine) QueryGate() string { return e.gate }

type gateResolver struct{ engine *gateEngine }

func (r gateResolver) Resolve(context.Context, string, string) (Lease, error) {
	return Lease{Engine: r.engine, Release: "test", Done: func() {}}, nil
}

func TestLegacyGateEarlyRejection(t *testing.T) {
	for _, recipe := range []retrieval.Recipe{retrieval.Recipe2(), retrieval.Recipe3(), retrieval.Recipe4()} {
		for _, shape := range []string{"plain", "filter", "cursor"} {
			e := &gateEngine{fakeEngine: &fakeEngine{}, gate: recipe.QueryGate}
			cfg := Config{Resolver: gateResolver{e}, Clock: time.Now}
			in := searchInput{Country: "vn", Query: "ngan hang"}
			if shape == "filter" {
				in.Documents = []string{"vn:3"}
			}
			if shape == "cursor" {
				in.Cursor = "token"
			}
			got, err := cfg.search(context.Background(), in)
			if err != nil {
				t.Fatal(err)
			}
			early := recipe.Version != 4 && shape == "plain"
			if early {
				if e.calls.Load() != 0 || !got.Abstain || got.Release != "test" || got.Reason != "diacritics_required" || got.Message != "Vui lòng nhập câu hỏi tiếng Việt có dấu hoặc số hiệu văn bản." {
					t.Fatalf("recipe %d %s: %+v calls %d", recipe.Version, shape, got, e.calls.Load())
				}
			} else if e.calls.Load() != 2 {
				t.Fatalf("recipe %d %s: calls %d", recipe.Version, shape, e.calls.Load())
			}
		}
	}
}
