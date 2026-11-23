//go:build integration

package classify

import (
	"encoding/json"
	"strings"
	"testing"

	"danny.vn/trachuan/apps/corpus/internal/artifact"
	"danny.vn/trachuan/internal/testpg"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestClassificationGate_VerifiedPackedReferencesOnTarget(t *testing.T) {
	ctx := t.Context()
	taxonomy := strings.Repeat("a", 64)
	doc, err := NewOpenAIRecipe("gpt-6.2-sol", taxonomy)
	if err != nil {
		t.Fatal(err)
	}
	article, err := NewArticleRecipe(doc, strings.Repeat("b", 64))
	if err != nil {
		t.Fatal(err)
	}
	policy, hash, err := packPolicy(doc, article, nil)
	if err != nil {
		t.Fatal(err)
	}
	set := ClassificationSet{
		Version: 1, Source: ClassificationBuild{Country: "vn"}, Taxonomy: taxonomy, Policy: policy, PolicyHash: hash,
		Baseline: ClassificationBuild{Country: "vn", Release: PackBaselineRelease, InputHash: strings.Repeat("c", 64), RecipeHash: strings.Repeat("d", 64)},
	}
	pins, err := readGateLabels(set)
	if err != nil {
		t.Fatal(err)
	}
	for _, pin := range pins {
		ref := ClassificationReference{Slice: pin.slice, Provider: pin.provider, LabelsHash: pin.hash}
		seen := map[string]bool{}
		for _, label := range pin.labels.Articles {
			if !seen[label.Key] {
				seen[label.Key] = true
				ref.Documents = append(ref.Documents, PackedDocument{Key: label.Key, RequestHash: strings.Repeat("e", 64), Outcome: "no_family"})
			}
			ref.Articles = append(ref.Articles, PackedArticle{Key: label.Key, Anchor: label.Anchor, OwnTextSHA256: label.OwnTextSHA256, Outcome: "no_family"})
		}
		set.References = append(set.References, ref)
	}
	raw, err := json.Marshal(set)
	if err != nil {
		t.Fatal(err)
	}
	store := artifact.Store{Root: t.TempDir()}
	selected, err := writePackSet(ctx, store, raw)
	if err != nil {
		t.Fatal(err)
	}
	set, err = ReadClassificationSet(ctx, store, selected, "vn", taxonomy)
	if err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, testpg.NewDatabase(t))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if _, err = pool.Exec(ctx, `CREATE TABLE document (id bigint PRIMARY KEY, key text NOT NULL UNIQUE);
CREATE TABLE provision (document_id bigint NOT NULL, anchor text NOT NULL, text text NOT NULL, PRIMARY KEY(document_id, anchor));
CREATE TABLE taxonomy_node (id bigint PRIMARY KEY, code text NOT NULL, name text NOT NULL, parent_id bigint, level int NOT NULL, status text NOT NULL, origin text NOT NULL, path text NOT NULL);`); err != nil {
		t.Fatal(err)
	}
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	gate := ScoreClassificationSet(ctx, tx, set)
	if gate.Status != "unavailable" || gate.PolicySHA256 != hash || gate.TaxonomyVersion != taxonomy || len(gate.Slices) != 3 {
		t.Fatalf("gate did not score all pinned references: %+v", gate)
	}
	for i, want := range []struct {
		name    string
		entries int
	}{{"seeded", 300}, {"multi-family", 100}, {"post-2023", 100}} {
		slice := gate.Slices[i]
		if slice.Slice != want.name || slice.Status != "unavailable" || slice.Entries != want.entries || slice.InvalidEntries != want.entries || slice.Level2 == nil || !strings.Contains(strings.Join(slice.Reasons, "; "), "more than 5 percent") {
			t.Fatalf("unexpected score on a target without the references: %+v", slice)
		}
	}
}
