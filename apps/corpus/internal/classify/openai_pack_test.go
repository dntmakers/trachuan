package classify

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"danny.vn/trachuan/apps/corpus/internal/artifact"
	"danny.vn/trachuan/apps/corpus/internal/taxonomy"
)

func packFixture(t *testing.T, baseline, skip bool) (PackBuild, packSelection, string, string) {
	t.Helper()
	return packFixtureWithDocumentCount(t, baseline, skip, 100)
}

func packFixtureWithDocumentCount(t *testing.T, baseline, skip bool, count int64) (PackBuild, packSelection, string, string) {
	t.Helper()
	o, docs, menu, nodes, wire := trialFixture(t)
	wire.count = count
	root := t.TempDir()
	release := o.Release
	if baseline {
		release = PackBaselineRelease
	}
	label := taxonomy.LabelArticle{Key: docs[0].Key, Anchor: "a1", OwnTextSHA256: digest([]byte("own text"))}
	b := PackBuild{Country: o.Country, Release: release, InputHash: strings.Repeat("c", 64), RecipeHash: strings.Repeat("d", 64), Taxonomy: o.Recipe.TaxonomyHash, Articles: []taxonomy.LabelArticle{label}, Nodes: nodes, Menu: menu}
	s := packSelection{build: b, articles: b.Articles, hash: o.LabelsHash}
	if !baseline {
		_, hash, err := UnseededSelection(b.Country, b.Release, b.Articles, 1, 1)
		if err != nil {
			t.Fatal(err)
		}
		s.hash, err = UnseededBuildHash(hash, b.InputHash, b.RecipeHash)
		if err != nil {
			t.Fatal(err)
		}
		s.partition = "unseeded:1/1"
		o.Unseeded, o.Selection, o.SelectedArticles, o.SelectedDocuments = true, s.partition, 1, 1
	}
	o.LabelsHash, o.Release = s.hash, release
	docDir, articleDir := filepath.Join(root, "n1-of-1-documents"), filepath.Join(root, "n1-of-1-articles")
	o.Out, wire.out = docDir, docDir
	if skip {
		wire.result = `{"codes":[]}`
	}
	docBatch, articleBatch := "batch-one", "batch-two"
	if baseline {
		docBatch, articleBatch = "batch-three", "batch-four"
	}
	docClient := NewOpenAITrialClient("fixture-credential", "fixture_project", packArticleWire{wire, docBatch})
	if _, err := RunOpenAIDocumentTrial(t.Context(), o, docClient, docs, menu); err != nil {
		t.Fatal(err)
	}
	families, hash, err := ReadFamilyArtifact(filepath.Join(docDir, "families.json"), b.Taxonomy, nodes)
	if err != nil {
		t.Fatal(err)
	}
	o.Recipe, err = NewArticleRecipe(o.Recipe, hash)
	if err != nil {
		t.Fatal(err)
	}
	o.Out, wire.out = articleDir, articleDir
	wire.count = 100
	wire.result = `{"a1":{"codes":[{"code":"F","confidence":0}]}}`
	client := NewOpenAITrialClient("fixture-credential", "fixture_project", packArticleWire{wire, articleBatch})
	if _, err = RunOpenAIArticleTrial(t.Context(), o, client, []Article{{DocumentKey: label.Key, Anchor: label.Anchor, Heading: "Điều 1", Text: "own text", OwnText: "own text"}}, families, nodes); err != nil {
		t.Fatal(err)
	}
	return b, s, docDir, articleDir
}

type packArticleWire struct {
	*trialWire
	batch string
}

func (w packArticleWire) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Path == "/v1/batches/"+w.batch {
		r.URL.Path = "/v1/batches/batch-one"
	}
	resp, err := w.trialWire.RoundTrip(r)
	if err != nil || !strings.Contains(r.URL.Path, "/v1/batches") {
		return resp, err
	}
	raw, readErr := io.ReadAll(resp.Body)
	if closeErr := resp.Body.Close(); closeErr != nil {
		return nil, closeErr
	}
	if readErr != nil {
		return nil, readErr
	}
	raw = bytes.ReplaceAll(raw, []byte(`"batch-one"`), []byte(fmt.Sprintf("%q", w.batch)))
	resp.Body, resp.ContentLength = io.NopCloser(bytes.NewReader(raw)), int64(len(raw))
	return resp, nil
}

func TestPackPair(t *testing.T) {
	for _, tc := range []struct{ baseline, skip bool }{{false, false}, {true, true}} {
		t.Run(fmt.Sprint(tc), func(t *testing.T) {
			b, s, doc, articles := packFixture(t, tc.baseline, tc.skip)
			pair, err := verifyPackPair(doc, articles, s)
			if err != nil {
				t.Fatal(err)
			}
			got := pair.Articles[0]
			if len(pair.Articles) != 1 || len(pair.Documents) != 1 || got.OwnTextSHA256 != b.Articles[0].OwnTextSHA256 || (tc.skip && got.Outcome != "no_family") || (!tc.skip && (len(got.Codes) != 1 || got.Codes[0].Confidence != 0)) {
				t.Fatalf("unexpected packed evidence: %+v", pair)
			}
		})
	}
}

func TestPackRejectsInvalidEvidence(t *testing.T) {
	for _, name := range []string{"missing", "swapped", "capture", "selection", "unsettled", "unpriced", "ambiguous_input", "output", "cost", "missing_cost_counter", "extra_cost_field", "unexpected_part"} {
		t.Run(name, func(t *testing.T) {
			b, _, doc, articles := packFixture(t, false, false)
			switch name {
			case "missing":
				if err := os.RemoveAll(articles); err != nil {
					t.Fatal(err)
				}
			case "swapped":
				swap := filepath.Join(filepath.Dir(doc), "swap")
				for _, p := range [][2]string{{doc, swap}, {articles, doc}, {swap, articles}} {
					if err := os.Rename(p[0], p[1]); err != nil {
						t.Fatal(err)
					}
				}
			case "capture":
				packWriteTest(t, doc, "upload.capture", []byte("changed"))
			case "selection":
				b.Articles[0].OwnTextSHA256 = strings.Repeat("c", 64)
			case "unsettled":
				packReplaceTest(t, articles, "state.json", `"phase": "settled"`, `"phase": "terminal"`)
			case "unpriced":
				packReplaceTest(t, articles, "state.json", `"phase": "settled"`, `"phase": "settled", "unpriced_results": 1`)
			case "ambiguous_input":
				old := trialRead(t, doc, "input.jsonl")
				newInput := bytes.Replace(old, []byte(`"custom_id":"0"`), []byte(`"custom_id":"0","custom_id":"0"`), 1)
				packWriteTest(t, doc, "input.jsonl", newInput)
				packReplaceTest(t, doc, "state.json", digest(old), digest(newInput))
			case "output":
				packReplaceTest(t, articles, "articles.json", `"confidence": 0`, `"confidence": 1`)
				packWriteTest(t, articles, "MANIFEST.sha256", []byte(digest(trialRead(t, articles, "articles.json"))+"  articles.json\n"))
			case "extra_cost_field":
				packReplaceTest(t, articles, "cost.json", `"unpriced": [],`, `"unpriced": [], "unknown": {},`)
			case "missing_cost_counter":
				packReplaceTest(t, articles, "cost.json", `"priced_input_tokens": 101,`, `"paid_input_tokens": 101,`)
			case "unexpected_part":
				if err := os.Mkdir(filepath.Join(filepath.Dir(doc), "n01-of-1-documents"), 0700); err != nil {
					t.Fatal(err)
				}
			case "cost":
				packReplaceTest(t, articles, "cost.json", `"counted_input_tokens": 100`, `"counted_input_tokens": 101`)
			}
			store := artifact.Store{Root: t.TempDir()}
			_, err := packOpenAI(t.Context(), filepath.Dir(doc), 1, b, nil, store)
			if err == nil || strings.Contains(err.Error(), "baseline") {
				t.Fatalf("expected run evidence to fail before checking baseline: %v", err)
			}
			trialAbsent(t, store.Root, "classification-set")
		})
	}
}

func TestPackRepeatIsByteIdentical(t *testing.T) {
	b, _, doc, _ := packFixture(t, false, false)
	_, bs, baselineDoc, baselineArticle := packFixture(t, true, true)
	refs := []packReference{{name: "seeded", selection: bs, docDir: baselineDoc, articleDir: baselineArticle, scoring: gateLabels{slice: "seeded", provider: "package", hash: bs.hash, labels: taxonomy.Labels{Articles: bs.articles}}}}
	store := artifact.Store{Root: t.TempDir()}
	var first []byte
	for i := range 2 {
		hash, err := packOpenAI(t.Context(), filepath.Dir(doc), 1, b, refs, store)
		if err != nil {
			t.Fatal(err)
		}
		raw := trialRead(t, filepath.Join(store.Root, "classification-set", hash), "classification.json")
		if digest(raw) != hash || (i == 1 && !bytes.Equal(raw, first)) || bytes.Contains(raw, []byte("own text")) || bytes.Contains(raw, []byte("Luật thử nghiệm")) {
			t.Fatalf("invalid packed artifact: %s", hash)
		}
		set, err := ReadClassificationSet(t.Context(), store, hash, b.Country, b.Taxonomy)
		if err != nil || len(set.Articles) != 1 || len(set.References) != 1 {
			t.Fatalf("pack reader returned %d articles and %d references: %v", len(set.Articles), len(set.References), err)
		}
		first = raw
	}
}

func packWriteTest(t *testing.T, dir, name string, raw []byte) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), raw, 0600); err != nil {
		t.Fatal(err)
	}
}

func packReplaceTest(t *testing.T, dir, name, old, replacement string) {
	t.Helper()
	raw := trialRead(t, dir, name)
	out := bytes.Replace(raw, []byte(old), []byte(replacement), 1)
	if bytes.Equal(raw, out) {
		t.Fatalf("%s does not contain test marker %s", name, old)
	}
	packWriteTest(t, dir, name, out)
}

func TestPackRefusesExistingDifferentSet(t *testing.T) {
	store := artifact.Store{Root: t.TempDir()}
	data := []byte(`{"version":1}`)
	dir := filepath.Join(store.Root, ClassificationSetNamespace, digest(data))
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	existing := []byte(`{"version":2}`)
	packWriteTest(t, dir, ClassificationSetFilename, existing)
	if _, err := writePackSet(t.Context(), store, data); err == nil {
		t.Fatal("replaced different set")
	}
	if !bytes.Equal(trialRead(t, dir, ClassificationSetFilename), existing) {
		t.Fatal("changed existing set")
	}
}

func TestPackPolicy_HashesJSONObjectTuple(t *testing.T) {
	menu := []Family{{Code: "2.1"}, {Code: "1.1"}}
	policy, got, err := packPolicy(Recipe{Model: "model"}, Recipe{Article: &ArticleRecipe{FamiliesHash: strings.Repeat("a", 64)}}, menu)
	if err != nil {
		t.Fatal(err)
	}
	document, err := json.Marshal(policy.Document)
	if err != nil {
		t.Fatal(err)
	}
	orderedMenu, err := json.Marshal(menu)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(policy.Article, []byte(`{`)) || bytes.Contains(policy.Article, []byte(`"families_sha256"`)) {
		t.Fatalf("article policy is not an object stripped of the family file hash: %s", policy.Article)
	}
	want := digest([]byte(fmt.Sprintf("[%s,%s,%s]", document, policy.Article, orderedMenu)))
	if got != want {
		t.Fatalf("policy hash=%s, want canonical object tuple %s", got, want)
	}
}
