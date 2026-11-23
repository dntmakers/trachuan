package classify

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"danny.vn/trachuan/apps/corpus/internal/artifact"
)

func TestReadClassificationSet_VerifiesStoreBytesAndBinding(t *testing.T) {
	ctx := context.Background()
	store := artifact.Store{Root: t.TempDir()}
	taxonomy := strings.Repeat("a", 64)
	policy := ClassificationPolicy{Article: json.RawMessage(`{}`)}
	tuple, err := json.Marshal([3]any{policy.Document, policy.Article, policy.Menu})
	if err != nil {
		t.Fatal(err)
	}
	set := ClassificationSet{Version: 1, Source: ClassificationBuild{Country: "vn"}, Taxonomy: taxonomy, Policy: policy, PolicyHash: digest(tuple)}
	raw, err := json.Marshal(set)
	if err != nil {
		t.Fatal(err)
	}
	sum, err := writePackSet(ctx, store, raw)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ReadClassificationSet(ctx, store, sum, "vn", taxonomy)
	if err != nil || got.PolicyHash != set.PolicyHash {
		t.Fatalf("set=%+v err=%v", got, err)
	}
	path := filepath.Join(store.Abs(ClassificationSetNamespace), sum, ClassificationSetFilename)
	for _, tc := range []struct{ name, selection, country, taxonomy string }{
		{"path", path, "vn", taxonomy},
		{"missing", strings.Repeat("b", 64), "vn", taxonomy},
		{"wrong country", sum, "sg", taxonomy},
		{"wrong taxonomy", sum, "vn", strings.Repeat("c", 64)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ReadClassificationSet(ctx, store, tc.selection, tc.country, tc.taxonomy); err == nil {
				t.Fatal("accepted invalid selection")
			}
		})
	}
	if err := os.WriteFile(path, append(raw, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadClassificationSet(ctx, store, sum, "vn", taxonomy); err == nil || !strings.Contains(err.Error(), "SHA-256") {
		t.Fatalf("changed bytes: %v", err)
	}
}
