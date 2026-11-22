//go:build integration

package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"danny.vn/trachuan/apps/corpus/internal/evaluation"
	"danny.vn/trachuan/internal/corpusdb"
	"danny.vn/trachuan/internal/embedding"
	"danny.vn/trachuan/internal/retrieval"
	"danny.vn/trachuan/internal/retrieval/lexical"
	"danny.vn/trachuan/internal/testpg"
	"github.com/jackc/pgx/v5/pgxpool"
)

type warningTexts struct{ done bool }

func (w *warningTexts) Next(context.Context) (string, bool, error) {
	if w.done {
		return "", false, nil
	}
	w.done = true
	return "ngân hàng khoản", true, nil
}

type warningEmbedder struct{}

func (warningEmbedder) Recipe(context.Context) (embedding.Recipe, error) {
	return embedding.QueryRecipe1(), nil
}
func (warningEmbedder) Embed(context.Context, string) (embedding.Vector, error) {
	values := make([]float32, 1024)
	values[0] = 1
	return embedding.Vector{Values: values, TokenCount: 1, RecipeHash: embedding.QueryRecipe1().Hash()}, nil
}

func warningIntegrationSuite(c evaluation.WarningCase) evaluation.Suite {
	cases := []evaluation.Case{{ID: "positive", Question: c.Question, ExpectedCitations: []evaluation.Expectation{{DocNumber: "n"}}}, {ID: "negative", Question: "Câu hỏi không", ExpectAbstain: true}, {ID: "reject1", Question: "khong dau mot", ExpectedCitations: []evaluation.Expectation{{DocNumber: "n"}}}, {ID: "reject2", Question: "khong dau hai", ExpectedCitations: []evaluation.Expectation{{DocNumber: "n"}}}, {ID: "reject3", Question: "khong dau ba", ExpectedCitations: []evaluation.Expectation{{DocNumber: "n"}}}}
	var entries []evaluation.MapEntry
	for _, item := range cases {
		if len(item.ExpectedCitations) == 0 {
			continue
		}
		entries = append(entries, evaluation.MapEntry{CaseID: item.ID, CitationIndex: 0, State: "resolved", ExpectedNumber: "n", Reason: "reviewed", ReviewedAt: "2026-11-09", Evidence: []evaluation.Evidence{{FetchSHA256: "hash", QuotedSpan: "evidence"}}, Alternatives: []evaluation.Alternative{{Country: "vn", DocumentKey: "vn:1", Kind: "document"}}})
	}
	return evaluation.Suite{WarningCases: []evaluation.WarningCase{c}, Legacy: cases, Rejections: []string{"reject1", "reject2", "reject3"}, Hashes: evaluation.FixtureHashes{Legacy: "fixture"}, Map: evaluation.IdentityMap{Version: 1, LegacySHA256: "fixture", IdentityInventoryHash: "inventory", Entries: entries}}
}

func warningProductFixture(t *testing.T) (*pgxpool.Pool, *retrieval.Engine) {
	t.Helper()
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, testpg.NewDatabase(t, "vector", "ltree"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err = corpusdb.Apply(ctx, pool, 1024); err != nil {
		t.Fatal(err)
	}
	hash := strings.Repeat("a", 64)
	for i := 1; i <= 2; i++ {
		_, err = pool.Exec(ctx, `INSERT INTO document (id,country,key,number,number_norm,title,doc_type,issuer,class,source,validity_conflict,eff_from,eff_to,raw_issue_date,raw_eff_from,raw_eff_to,status_code,status_name,status_class,status_basis,is_current,is_consolidated,content_state,fetch_sha256,text_source,text_complete) VALUES ($1,'vn',$2,'n','n','title','law','issuer','primary','vbpl',false,'2020-01-01',NULL,'','','','','','in_force','source_status',true,false,'indexed',$3,'source_tree',true)`, i, fmt.Sprintf("vn:%d", i), hash)
		if err != nil {
			t.Fatal(err)
		}
		anchor := "dieu-5"
		if i == 2 {
			anchor = "dieu-30"
		}
		_, err = pool.Exec(ctx, `INSERT INTO provision (id,document_id,parent_id,node_id,kind,label,heading,ordinal,preorder,path,text,anchor) VALUES ($1,$2,NULL,$3,'dieu',$3,'',1,1,$4::ltree,'ngân hàng khoản',$3)`, i*10+1, i, anchor, fmt.Sprintf("d%d", i))
		if err != nil {
			t.Fatal(err)
		}
	}
	stats, err := lexical.Train(ctx, &warningTexts{}, lexical.Recipe1())
	if err != nil {
		t.Fatal(err)
	}
	sparse, err := lexical.Document("ngân hàng khoản", stats, lexical.Recipe1())
	if err != nil {
		t.Fatal(err)
	}
	var sparseText strings.Builder
	sparseText.WriteByte('{')
	for i, v := range sparse {
		if i > 0 {
			sparseText.WriteByte(',')
		}
		fmt.Fprintf(&sparseText, "%d:%s", v.Index, strconv.FormatFloat(float64(v.Value), 'g', -1, 32))
	}
	fmt.Fprintf(&sparseText, "}/%d", lexical.Recipe1().Dimensions)
	vector := "[1," + strings.Repeat("0,", 1022) + "0]"
	_, err = pool.Exec(ctx, `INSERT INTO chunk (id,document_id,provision_id,ordinal,context_prefix,content,spans,input_sha256,token_count,embedding,sparse) VALUES (1,1,11,1,'','ngân hàng khoản','[]',$1,1,$2::vector,$3::sparsevec)`, hash, vector, sparseText.String())
	if err != nil {
		t.Fatal(err)
	}
	q, d := embedding.QueryRecipe1(), embedding.DocumentRecipe1()
	dense := retrieval.DenseRecipe{Version: 1, Model: q.Model, Dimensions: q.Dimensions, Document: d, DocumentHash: d.Hash(), Query: q, QueryHash: q.Hash()}
	recipe := retrieval.Recipe2()
	threshold := .1
	recipe.Abstention.Threshold = &threshold
	recipe.Abstention.CalibrationSHA256 = hash
	marshal := func(v any) string {
		b, marshalErr := json.Marshal(v)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		return string(b)
	}
	_, err = pool.Exec(ctx, `INSERT INTO release VALUES (true,'vn','test',$6,'v1',$1::jsonb,'{}',$2::jsonb,$3::jsonb,$4::jsonb,$5,$5,$5,'{}',now())`, marshal(dense), marshal(lexical.Recipe1()), marshal(recipe), marshal(stats.Summary(lexical.Recipe1(), nil)), hash, corpusdb.SchemaVersion)
	if err != nil {
		t.Fatal(err)
	}
	engine, err := retrieval.Open(ctx, pool, warningEmbedder{}, retrieval.Options{Now: func() time.Time { return time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC) }, Profile: "evaluation"})
	if err != nil {
		t.Fatal(err)
	}
	return pool, engine
}
