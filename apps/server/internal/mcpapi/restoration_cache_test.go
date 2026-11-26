package mcpapi

import (
	"context"
	"testing"
	"time"

	"danny.vn/trachuan/internal/retrieval"
)

type recipeCacheEngine struct {
	*fakeEngine
	recipe retrieval.Recipe
}

func (e *recipeCacheEngine) QueryGate() string {
	return e.recipe.QueryGate
}

type recipeCacheResolver struct{ e *recipeCacheEngine }

func (r recipeCacheResolver) Resolve(context.Context, string, string) (Lease, error) {
	return Lease{Engine: r.e, Release: "test", Generation: 1, Done: func() {}}, nil
}

func TestRestorationBypassesResponseCache(t *testing.T) {
	clock := time.Now
	c := NewCaches(clock)
	c.ActivateRelease("test", 1)
	e := &recipeCacheEngine{fakeEngine: &fakeEngine{}, recipe: retrieval.Recipe3()}
	cfg := Config{Resolver: recipeCacheResolver{e}, Caches: c, Clock: clock}
	if _, _, err := cfg.cachedSearch(context.Background(), searchInput{Country: "vn", Query: "ngan hang"}, func(any) error { return nil }); err != nil {
		t.Fatal(err)
	}
	e.recipe = retrieval.Recipe4()
	before := c.Search.Stats()
	for i := 0; i < 2; i++ {
		if _, _, err := cfg.cachedSearch(context.Background(), searchInput{Country: "vn", Query: "ngan hang"}, func(any) error { return nil }); err != nil {
			t.Fatal(err)
		}
	}
	if after := c.Search.Stats(); after != before {
		t.Fatalf("restoration accessed cache: before %+v after %+v", before, after)
	}
	for i := 0; i < 2; i++ {
		if _, _, err := cfg.cachedSearch(context.Background(), searchInput{Country: "vn", Query: "ngân hàng"}, func(any) error { return nil }); err != nil {
			t.Fatal(err)
		}
	}
	if after := c.Search.Stats(); after.Hits != before.Hits+1 || after.Misses != before.Misses+1 || after.Bytes == 0 {
		t.Fatalf("accented cache: %+v", after)
	}
}

func TestRestoredResponseIsNotCached(t *testing.T) {
	c := NewCaches(time.Now)
	c.ActivateRelease("test", 1)
	e := &recipeCacheEngine{fakeEngine: &fakeEngine{presented: &retrieval.SearchResult{RestoredQuery: "ngân hàng"}}, recipe: retrieval.Recipe4()}
	cfg := Config{Resolver: recipeCacheResolver{e}, Caches: c, Clock: time.Now}
	for i := 0; i < 2; i++ {
		if _, _, err := cfg.cachedSearch(context.Background(), searchInput{Country: "vn", Query: "ngân hàng"}, func(any) error { return nil }); err != nil {
			t.Fatal(err)
		}
	}
	if stats := c.Search.Stats(); stats.Hits != 0 || stats.Misses != 2 || stats.Bytes != 0 {
		t.Fatalf("restored response cached: %+v", stats)
	}
}
