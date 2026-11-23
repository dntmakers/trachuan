//go:build integration

package build

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"strings"
	"testing"

	"danny.vn/trachuan/apps/corpus/internal/classify"
	"danny.vn/trachuan/apps/corpus/internal/stage/chunk"
	"danny.vn/trachuan/apps/corpus/internal/taxonomy"
	"github.com/jackc/pgx/v5"
)

func TestLoad_ClassificationWritesExactRowsAndOneGapPerArticle(t *testing.T) {
	ctx := context.Background()
	f := fixture(t)
	fetch := f.fetch(t, classificationKey)
	fragment := "Luật này có hiệu lực từ ngày 01/01/2026."
	var provisions []chunk.Node
	for i, text := range []string{classificationText, classificationText, classificationText, fragment, classificationText, classificationText} {
		anchor := fmt.Sprintf("dieu-%d", i+1)
		provisions = append(provisions, chunk.Node{NodeID: anchor, Kind: "dieu", Ordinal: i + 1, Path: anchor, Text: text})
	}
	f.addArtifact(t, "extract", classificationKey, map[string]any{"provisions.json": provisions, "report.json": []any{}}, fetch.SHA256)
	input := taxonomyInputFixture()
	input.overrides = []taxonomy.Override{{DocumentKey: classificationKey, Anchor: "dieu-5", Code: "1.1.c1", Exclude: true}}
	set := matchSet()
	set.Taxonomy = input.version
	set.Policy.Menu = set.Policy.Menu[:1]
	set.Documents[0].Codes = []string{"1.1"}
	set.Articles[0].Codes = []classify.CodeConfidence{{Code: "1.1.c1", Confidence: 0}}
	other := set.Documents[0]
	other.Key = "vn:vbpl:2"
	set.Documents = append(set.Documents, other)
	for _, a := range []struct{ key, anchor, text string }{
		{classificationKey, "dieu-20", classificationText},
		{classificationKey, "dieu-3", "Changed legal text"},
		{classificationKey, "dieu-4", fragment},
		{classificationKey, "dieu-5", classificationText},
		{other.Key, "dieu-6", classificationText},
	} {
		set.Articles = append(set.Articles, classify.PackedArticle{Key: a.key, Anchor: a.anchor, OwnTextSHA256: taxonomy.OwnTextSHA256(a.text), Outcome: "ok", Codes: set.Articles[0].Codes})
	}
	counts, err := Load(ctx, f.corpus, f.inventory(t), f.artifacts, f.recipes, nil, nil, f.asOf(), LoadOptions{Taxonomy: input, classification: &set})
	if err != nil {
		t.Fatal(err)
	}
	var anchor, code, version string
	var exclude bool
	var confidence float64
	if err := f.corpus.QueryRow(ctx, `SELECT p.anchor,n.code,a.taxonomy_version,a.exclude,a.confidence FROM taxonomy_assignment a JOIN provision p ON p.id=a.provision_id JOIN taxonomy_node n ON n.id=a.node_id WHERE a.method='llm'`).Scan(&anchor, &code, &version, &exclude, &confidence); err != nil || anchor != "dieu-1" || code != "1.1" || version != input.version || exclude || confidence != 0 {
		t.Fatalf("row=%s %s %s %t %v: %v", anchor, code, version, exclude, confidence, err)
	}
	rows, err := f.corpus.Query(ctx, `SELECT item_key || ' ' || detail FROM coverage_gap WHERE kind='unassigned' ORDER BY detail`)
	if err != nil {
		t.Fatal(err)
	}
	got, err := pgx.CollectRows(rows, pgx.RowTo[string])
	want := []string{classificationKey + " dieu-2 missing_result", classificationKey + " dieu-3 changed_own_text", classificationKey + " dieu-6 missing_result"}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("gaps=%v want=%v err=%v", got, want, err)
	}
	stats := counts.Taxonomy.ClassificationStats
	if stats == nil || stats.LLMRows != 1 || stats.LLMAssignedArticles != 1 || stats.LLMUnusedArticles != 5 || !maps.Equal(stats.LLMGaps, map[string]int64{"missing_result": 2, "changed_own_text": 1}) {
		t.Fatalf("stats=%+v", stats)
	}
	record := BuildRecord{Taxonomy: &counts.Taxonomy, TaxonomyVersion: input.version, ClassificationSetSHA256: strings.Repeat("a", 64), classification: &set}
	if err := validateTaxonomy(ctx, f.corpus, record); err != nil {
		t.Fatalf("validate classification: %v", err)
	}
	stats.LLMRows++
	if err := validateTaxonomy(ctx, f.corpus, record); !errors.Is(err, errInvalidDatabase) {
		t.Fatalf("incorrect row count accepted: %v", err)
	}
	stats.LLMRows--
	if _, err := f.corpus.Exec(ctx, `UPDATE taxonomy_node SET status='retired' WHERE code='1.1'`); err != nil {
		t.Fatal(err)
	}
	if err := validateClassificationDatabase(ctx, f.corpus, record); !errors.Is(err, errInvalidDatabase) {
		t.Fatalf("inactive node accepted: %v", err)
	}
	if _, err := f.corpus.Exec(ctx, `UPDATE taxonomy_node SET status='active' WHERE code='1.1'; UPDATE taxonomy_assignment SET provision_id=(SELECT id FROM provision WHERE anchor='dieu-4') WHERE method='llm'`); err != nil {
		t.Fatal(err)
	}
	if err := validateClassificationDatabase(ctx, f.corpus, record); !errors.Is(err, errInvalidDatabase) {
		t.Fatalf("fragment assignment accepted: %v", err)
	}
}

func TestLoad_BadClassificationRollsBack(t *testing.T) {
	ctx := context.Background()
	f := fixture(t)
	taxonomyDocument(t, f)
	input := taxonomyInputFixture()
	set := matchSet()
	set.Taxonomy = input.version
	set.Policy.Menu = set.Policy.Menu[:1]
	set.Documents[0].Codes = []string{"1.1"}
	set.Articles[0].Codes = []classify.CodeConfidence{{Code: "1.1.c2", Confidence: 0.5}}
	_, err := Load(ctx, f.corpus, f.inventory(t), f.artifacts, f.recipes, nil, nil, f.asOf(), LoadOptions{Taxonomy: input, classification: &set})
	if err == nil || !strings.Contains(err.Error(), "1.1.c2") {
		t.Fatalf("bad unused result was accepted: %v", err)
	}
	var docs, nodes int
	if err = f.corpus.QueryRow(ctx, `SELECT (SELECT count(*) FROM document), (SELECT count(*) FROM taxonomy_node)`).Scan(&docs, &nodes); err != nil || docs != 0 || nodes != 0 {
		t.Fatalf("not rolled back: documents=%d nodes=%d err=%v", docs, nodes, err)
	}
}

func TestClassificationValidation_RejectsChangedEvidence(t *testing.T) {
	ctx := context.Background()
	f := fixture(t)
	fetch := f.fetch(t, classificationKey)
	var provisions []chunk.Node
	set := matchSet()
	set.Articles = nil
	for i, outcome := range []string{"ok", "ok", "too_long", "ok"} {
		anchor := fmt.Sprintf("dieu-%d", i+1)
		provisions = append(provisions, chunk.Node{NodeID: anchor, Kind: "dieu", Ordinal: i + 1, Path: anchor, Text: classificationText})
		article := classify.PackedArticle{Key: classificationKey, Anchor: anchor, OwnTextSHA256: taxonomy.OwnTextSHA256(classificationText), Outcome: outcome}
		if i < 2 {
			article.Codes = []classify.CodeConfidence{{Code: fmt.Sprintf("1.%d", i+1), Confidence: 0.6}}
		}
		set.Articles = append(set.Articles, article)
	}
	f.addArtifact(t, "extract", classificationKey, map[string]any{"provisions.json": provisions, "report.json": []any{}}, fetch.SHA256)
	input := taxonomyInputFixture()
	input.mapping.Nodes = append(input.mapping.Nodes, taxonomy.MappedNode{GUID: "other", Code: "1.2", ParentCode: "1", Level: 2, Status: "active", Ordinal: 2, Name: "Other"})
	set.Taxonomy = input.version
	counts, err := Load(ctx, f.corpus, f.inventory(t), f.artifacts, f.recipes, nil, nil, f.asOf(), LoadOptions{Taxonomy: input, classification: &set})
	if err != nil {
		t.Fatal(err)
	}
	record := BuildRecord{Taxonomy: &counts.Taxonomy, TaxonomyVersion: input.version, ClassificationSetSHA256: strings.Repeat("a", 64), classification: &set}
	if err := validateTaxonomy(ctx, f.corpus, record); err != nil {
		t.Fatal(err)
	}
	const swapFamilies = `UPDATE taxonomy_assignment SET provision_id=3-provision_id WHERE method='llm'`
	const swapReasons = `UPDATE coverage_gap SET detail=CASE detail WHEN 'dieu-3 too_long' THEN 'dieu-3 no_code' ELSE 'dieu-4 too_long' END WHERE kind='unassigned'`
	const resetReasons = `UPDATE coverage_gap SET detail=CASE detail WHEN 'dieu-3 no_code' THEN 'dieu-3 too_long' ELSE 'dieu-4 no_code' END WHERE kind='unassigned'`
	for _, tc := range []struct{ name, mutate, reset string }{
		{"swapped families", swapFamilies, swapFamilies},
		{"changed confidence", `UPDATE taxonomy_assignment SET confidence=0 WHERE method='llm'`, `UPDATE taxonomy_assignment SET confidence=0.6 WHERE method='llm'`},
		{"swapped gap reasons", swapReasons, resetReasons},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := f.corpus.Exec(ctx, tc.mutate); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if _, err := f.corpus.Exec(ctx, tc.reset); err != nil {
					t.Fatal(err)
				}
			})
			if err := validateTaxonomy(ctx, f.corpus, record); !errors.Is(err, errInvalidDatabase) {
				t.Fatalf("changed evidence accepted: %v", err)
			}
		})
	}
}
