//go:build integration

package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"danny.vn/trachuan/apps/server/internal/contracttest"
	"danny.vn/trachuan/apps/server/internal/publicsample"
	"danny.vn/trachuan/apps/server/internal/publicsummary"
	"danny.vn/trachuan/apps/server/internal/release"
	"danny.vn/trachuan/internal/corpusdb"
	"danny.vn/trachuan/internal/embedding"
	"danny.vn/trachuan/internal/retrieval"
	"danny.vn/trachuan/internal/retrieval/lexical"
	"danny.vn/trachuan/internal/testpg"
	"github.com/jackc/pgx/v5/pgxpool"
)

type sampleFixtureTexts struct {
	values []string
	index  int
}

func (s *sampleFixtureTexts) Next(context.Context) (string, bool, error) {
	if s.index == len(s.values) {
		return "", false, nil
	}
	value := s.values[s.index]
	s.index++
	return value, true, nil
}

type sampleFixtureEmbedder struct{ calls atomic.Int64 }

func (*sampleFixtureEmbedder) Recipe(context.Context) (embedding.Recipe, error) {
	return embedding.QueryRecipe1(), nil
}
func (e *sampleFixtureEmbedder) Embed(context.Context, string) (embedding.Vector, error) {
	e.calls.Add(1)
	v := make([]float32, 1024)
	v[0] = 1
	return embedding.Vector{Values: v, TokenCount: 1, RecipeHash: embedding.QueryRecipe1().Hash()}, nil
}

type sampleFixtureStore string

func (s sampleFixtureStore) GetActiveRelease(context.Context, string) (string, error) {
	return string(s), nil
}

func sampleFixtureSparse(v lexical.Vector, dimensions int) string {
	var entries []string
	for _, entry := range v {
		entries = append(entries, strconv.FormatInt(int64(entry.Index), 10)+":"+strconv.FormatFloat(float64(entry.Value), 'g', -1, 32))
	}
	return "{" + strings.Join(entries, ",") + "}/" + strconv.Itoa(dimensions)
}

func makeSampleCorpus(t *testing.T, ctx context.Context, db *pgxpool.Pool, rel string) {
	t.Helper()
	if _, err := db.Exec(ctx, `CREATE EXTENSION IF NOT EXISTS vector; CREATE EXTENSION IF NOT EXISTS ltree`); err != nil {
		t.Fatal(err)
	}
	if err := corpusdb.Apply(ctx, db, 1024); err != nil {
		t.Fatal(err)
	}
	hash := strings.Repeat("a", 64)
	body := publicsample.Query + " " + strings.Repeat("Ngân hàng phải bảo vệ dữ liệu cá nhân của khách hàng theo quy định. ", 50)
	texts := &sampleFixtureTexts{values: []string{body}}
	stats, err := lexical.Train(ctx, texts, lexical.Recipe1())
	if err != nil {
		t.Fatal(err)
	}
	sparse, err := lexical.Document(body, stats, lexical.Recipe1())
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(ctx, `INSERT INTO document (id,country,key,number,number_norm,title,doc_type,issuer,class,source,validity_conflict,eff_from,raw_issue_date,raw_eff_from,raw_eff_to,status_code,status_name,status_class,status_basis,is_current,is_consolidated,content_state,fetch_sha256,text_source,text_complete) VALUES (1,'vn',$1,$2,$2,'Luật bảo vệ dữ liệu cá nhân','law','Quốc hội','primary','vbpl',false,'2000-01-01','','','','','','in_force','source_status',true,false,'indexed',$3,'source_tree',true)`, publicsample.DocumentKey, publicsample.DocumentNumber, hash)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(ctx, `INSERT INTO provision (id,document_id,parent_id,node_id,kind,label,heading,ordinal,preorder,path,text,anchor) VALUES (1,1,NULL,'dieu-1','dieu','Điều 1','',1,1,'a1','Quy định chung','dieu-1'), (2,1,1,'khoan-1','khoan','Khoản 1','',1,2,'a1.c1',$1,'dieu-1-khoan-1')`, body)
	if err != nil {
		t.Fatal(err)
	}
	vector := "[1," + strings.Repeat("0,", 1022) + "0]"
	spans := fmt.Sprintf(`[{"node_id":"khoan-1","start":0,"end":%d,"content_start":0,"content_end":%d,"role":"body"}]`, len([]rune(body)), len([]rune(body)))
	_, err = db.Exec(ctx, `INSERT INTO chunk(id,document_id,provision_id,ordinal,context_prefix,content,spans,input_sha256,token_count,embedding,sparse) VALUES (1,1,2,0,'',$1,$2::jsonb,$3,1,$4::vector,$5::sparsevec)`, body, spans, hash, vector, sampleFixtureSparse(sparse, lexical.Recipe1().Dimensions))
	if err != nil {
		t.Fatal(err)
	}
	q, d := embedding.QueryRecipe1(), embedding.DocumentRecipe1()
	dense := retrieval.DenseRecipe{Version: 1, Model: q.Model, Dimensions: q.Dimensions, Document: d, DocumentHash: d.Hash(), Query: q, QueryHash: q.Hash()}
	recipe := retrieval.Recipe2()
	threshold := 0.1
	recipe.Abstention.Threshold = &threshold
	recipe.Abstention.CalibrationSHA256 = hash
	marshal := func(value any) string {
		encoded, e := json.Marshal(value)
		if e != nil {
			t.Fatal(e)
		}
		return string(encoded)
	}
	_, err = db.Exec(ctx, `INSERT INTO release VALUES (true,'vn',$6,$7,'v1',$1::jsonb,'{}',$2::jsonb,$3::jsonb,$4::jsonb,$5,$5,$5,'{}',now())`, marshal(dense), marshal(lexical.Recipe1()), marshal(recipe), marshal(stats.Summary(lexical.Recipe1(), nil)), hash, rel, corpusdb.SchemaVersion)
	if err != nil {
		t.Fatal(err)
	}
}

func TestPublicSample_PostgresSuccessAndPoolCapacity(t *testing.T) {
	ctx := context.Background()
	admin, base, err := testpg.OpenAdministrator(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(ctx)
	name, key, err := testpg.CreateCorpus(ctx, admin, t.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer testpg.DropCorpusDatabase(t, admin, name, key)
	base.Path = "/" + name
	reader, err := pgxpool.New(ctx, base.String())
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	rel := strings.TrimPrefix(name, "corpus_vn_")
	makeSampleCorpus(t, ctx, reader, rel)
	embedder := &sampleFixtureEmbedder{}
	clock := func() time.Time { return time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC) }
	engine, err := retrieval.Open(ctx, reader, embedder, retrieval.Options{Now: clock, Profile: "serving"})
	if err != nil {
		t.Fatal(err)
	}
	resolver := &release.Resolver{Auth: sampleFixtureStore(name), Clock: clock, Open: func(context.Context, string) (*retrieval.Engine, func(), error) { return engine, engine.Close, nil }}
	defer resolver.Close()
	lease, err := resolver.Resolve(ctx, "vn", "")
	if err != nil {
		t.Fatal(err)
	}
	lease.Done()
	base.Path = "/{database}"
	template := strings.Replace(base.String(), "%7Bdatabase%7D", "{database}", 1)
	pools := &samplePools{open: func(ctx context.Context, db string) (samplePool, error) {
		cfg, e := samplePoolConfig(template, db)
		if e != nil {
			return nil, e
		}
		return pgxpool.NewWithConfig(ctx, cfg)
	}}
	defer pools.Close()
	activation := publicsample.Activation{Database: name, ActivatedAt: clock()}
	pools.observe(activation)
	pool, err := pools.get(ctx, activation)
	if err != nil {
		t.Fatal(err)
	}
	pgpool := pool.(*pgxpool.Pool)
	conn, err := pgpool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	conn.Release()
	before := pgpool.Stat()
	if before.MaxConns() != 1 {
		t.Fatalf("max conns %d", before.MaxConns())
	}
	search := sampleSearch(pools, resolver.ReadyLaw, func(ctx context.Context, lease *release.Lease, pool samplePool, instant time.Time) ([]byte, error) {
		return publicsample.SearchSample(ctx, lease.Engine.WithPool(pool.(*pgxpool.Pool)), lease.Release, instant)
	})
	flights := publicsample.NewFlights(ctx, func(context.Context) (publicsample.Activation, error) { return activation, nil }, sampleReady(pools, resolver.ReadyLaw), search, clock)
	defer flights.Close()
	h := publicsample.NewHandler(true, publicsummary.NewAdmission()).WithFlights(flights)
	contract, err := contracttest.Load()
	if err != nil {
		t.Fatal(err)
	}
	coverage := contracttest.NewCoverage(contract, []string{"/api/v1/public/sample"})
	for i := range 2 {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/public/sample", nil))
		if w.Code != http.StatusOK || w.Header().Get("X-Sample-Status-Date") != "2099-01-01" {
			t.Fatalf("request %d: %d %s", i, w.Code, w.Body.String())
		}
		coverage.Check(t, "GET", "/api/v1/public/sample", w)
	}
	after := pgpool.Stat()
	if after.EmptyAcquireCount() != before.EmptyAcquireCount() || after.AcquireCount() <= before.AcquireCount() {
		t.Fatalf("acquisitions before %d/%d after %d/%d", before.EmptyAcquireCount(), before.AcquireCount(), after.EmptyAcquireCount(), after.AcquireCount())
	}
	if embedder.calls.Load() != 1 {
		t.Fatalf("embedding calls %d", embedder.calls.Load())
	}
}
