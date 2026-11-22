package build

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"danny.vn/trachuan/apps/corpus/internal/artifact"
	"danny.vn/trachuan/apps/corpus/internal/stage"
)

func TestBuildExtractAuthorityBothConsumers(t *testing.T) {
	ctx := context.Background()
	store := artifact.Store{Root: t.TempDir()}
	recipe := strings.Repeat("a", 64)
	publish := func(name, input string, files map[string]string) artifact.Result {
		t.Helper()
		result, err := store.Publish(ctx, name, input, recipe, func(dir string) error {
			for name, body := range files {
				if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0600); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	f := publish("fetch", strings.Repeat("b", 64), map[string]string{"detail.json": `{"data":{"id":"1"}}`, "files.json": `[]`})
	e := publish("extract", f.SHA256, map[string]string{"provisions.json": `invalid`})
	key := "vn:vbpl:1"
	fetchItem := InventoryItem{Stage: "fetch", ItemKey: key, State: "done", Path: &f.Path, ArtifactSHA256: &f.SHA256}
	extractItem := InventoryItem{Stage: "extract", ItemKey: key, State: "done", Path: &e.Path, ArtifactSHA256: &e.SHA256, InputHash: &f.SHA256, RecipeHash: &recipe, authority: testExtractAuthority(key, &f.SHA256, &recipe)}
	verified := readIdentityFetch(store, fetchItem)
	if verified.err != nil {
		t.Fatal(verified.err)
	}
	for _, tc := range []struct{ name, contract, declared, reason string }{
		{"missing", "", recipe, "contract"},
		{"mismatch", stage.ExtractRecordLegacy, strings.Repeat("c", 64), "recipe"},
		{"missing item recipe", stage.ExtractRecordLegacy, recipe, "recipe"},
		{"v1 missing record", stage.ExtractRecordV1, recipe, "text record"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			snapshot := stage.Snapshot{ExtractRecordContract: tc.contract, Recipes: map[string]string{"extract": tc.declared}}
			item := extractItem
			if tc.name == "missing item recipe" {
				item.RecipeHash = nil
			}
			inv := Inventory{Country: "vn", ExtractSnapshot: snapshot, ExpectedKeys: []string{key}, Items: []InventoryItem{fetchItem, item}}
			if _, err := IdentityInventory(inv, store, nil); err == nil || !strings.Contains(err.Error(), tc.reason) {
				t.Errorf("identity err=%v", err)
			}
			l := loader{tx: &noneTx{}, inv: inv, artifacts: store, verified: map[string]verifiedFetch{key: {document: verified.detail, dir: verified.dir, hash: f.SHA256}}, present: map[string]bool{key: true}, docs: map[string]*loadedDocument{}, items: map[string]map[string]InventoryItem{"fetch": {key: fetchItem}, "extract": {key: item}}}
			if err := l.loadDocuments(ctx); err == nil || !strings.Contains(err.Error(), tc.reason) {
				t.Errorf("documents err=%v", err)
			}
		})
	}
}

func TestBuildExtractAuthorityAcceptsMatchingRecords(t *testing.T) {
	for _, contract := range []string{stage.ExtractRecordLegacy, stage.ExtractRecordV1} {
		t.Run(contract, func(t *testing.T) {
			ctx := context.Background()
			store := artifact.Store{Root: t.TempDir()}
			recipe := strings.Repeat("a", 64)
			f, err := store.PublishContent(ctx, "fetch", recipe, func(dir string) error {
				if writeErr := os.WriteFile(filepath.Join(dir, "detail.json"), []byte(`{"data":{"id":"1"}}`), 0600); writeErr != nil {
					return writeErr
				}
				return os.WriteFile(filepath.Join(dir, "files.json"), []byte(`[]`), 0600)
			})
			if err != nil {
				t.Fatal(err)
			}
			e, err := store.Publish(ctx, "extract", f.SHA256, recipe, func(dir string) error {
				if writeErr := os.WriteFile(filepath.Join(dir, "provisions.json"), []byte(`[]`), 0600); writeErr != nil {
					return writeErr
				}
				if contract == stage.ExtractRecordV1 {
					return os.WriteFile(filepath.Join(dir, "text.json"), []byte(`{"text_source":"source_tree","detector_inputs":{"version":1,"ocr_check":{"state":"not_selected"},"tree_transcription":{"state":"no_pair"}}}`), 0600)
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			key := "vn:vbpl:1"
			fetchItem := InventoryItem{Stage: "fetch", ItemKey: key, State: "done", Path: &f.Path, ArtifactSHA256: &f.SHA256}
			extractItem := InventoryItem{Stage: "extract", ItemKey: key, State: "done", Path: &e.Path, ArtifactSHA256: &e.SHA256, InputHash: &f.SHA256, RecipeHash: &recipe, authority: testExtractAuthority(key, &f.SHA256, &recipe)}
			inv := Inventory{Country: "vn", ExtractSnapshot: stage.Snapshot{ExtractRecordContract: contract, Recipes: map[string]string{"extract": recipe}}, ExpectedKeys: []string{key}, Items: []InventoryItem{fetchItem, extractItem}}
			if _, err = IdentityInventory(inv, store, nil); err != nil {
				t.Fatalf("identity: %v", err)
			}
			verified := readIdentityFetch(store, fetchItem)
			if verified.err != nil {
				t.Fatal(verified.err)
			}
			tx := &noneTx{}
			l := loader{tx: tx, inv: inv, artifacts: store, verified: map[string]verifiedFetch{key: {document: verified.detail, dir: verified.dir, hash: f.SHA256}}, present: map[string]bool{key: true}, docs: map[string]*loadedDocument{}, items: map[string]map[string]InventoryItem{"fetch": {key: fetchItem}, "extract": {key: extractItem}}}
			if err = l.loadDocuments(ctx); err != nil || tx.inserts != 1 {
				t.Fatalf("documents inserts=%d err=%v", tx.inserts, err)
			}
		})
	}
}
