package release

import (
	"context"
	"testing"
)

func TestResolver_ReadyLawNeverOpens(t *testing.T) {
	r, _, engine := resolverFixture(t)
	if got := r.ReadyLaw("corpus_vn_20990103"); got != nil {
		got.Done()
		t.Fatal("cold engine was ready")
	}
	lease, err := r.Resolve(context.Background(), "vn", "")
	if err != nil {
		t.Fatal(err)
	}
	lease.Done()
	ready := r.ReadyLaw("corpus_vn_20990103")
	if ready == nil || ready.Engine != engine {
		t.Fatal("open engine was not ready")
	}
	ready.Done()
	if wrong := r.ReadyLaw("corpus_vn_20990104"); wrong != nil {
		wrong.Done()
		t.Fatal("wrong database was ready")
	}
}
