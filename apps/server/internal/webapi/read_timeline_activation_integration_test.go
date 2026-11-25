//go:build integration

package webapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"danny.vn/trachuan/apps/server/internal/limits"
	"danny.vn/trachuan/apps/server/internal/mcpapi"
	"danny.vn/trachuan/apps/server/internal/release"
	"danny.vn/trachuan/apps/server/internal/websession"
	"danny.vn/trachuan/internal/retrieval"
)

type timelineActivationStore struct{ active string }

func (s *timelineActivationStore) GetActiveRelease(context.Context, string) (string, error) {
	return s.active, nil
}

type timelineActivationResolver struct{ resolver *release.Resolver }

func (r timelineActivationResolver) Resolve(ctx context.Context, country, rel string) (mcpapi.Lease, error) {
	lease, err := r.resolver.Resolve(ctx, country, rel)
	if err != nil {
		return mcpapi.Lease{}, err
	}
	return mcpapi.Lease{Engine: lease.Engine, Release: lease.Release, Generation: lease.Generation, Done: lease.Done}, nil
}

func TestReadTimeline_ContinuationAcrossActivation(t *testing.T) {
	engine := taxonomyIntegrationEngine(t, `INSERT INTO relation(id,source_document_id,target_document_id,source_key,target_key,source_number,target_number,kind,raw_code,raw_label,evidence,method,artifact_sha256,ordinal)
	 SELECT n,NULL,1,'vn:actor'||lpad(n::text,3,'0'),'vn:1','N','n','repeals','','','','text_rule/1',repeat('b',64),n FROM generate_series(1,120) n`)
	newEngine := taxonomyIntegrationEngine(t)
	store := &timelineActivationStore{active: "corpus_vn_20991110_1"}
	now := time.Date(2099, 11, 10, 0, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	resolver := &release.Resolver{Auth: store, Clock: clock, Open: func(_ context.Context, name string) (*retrieval.Engine, func(), error) {
		if name == "corpus_vn_20991110_1" {
			return engine, func() {}, nil
		}
		return newEngine, func() {}, nil
	}}
	t.Cleanup(resolver.Close)
	cfg := mcpapi.Config{Resolver: timelineActivationResolver{resolver: resolver}, Limiter: limits.New(clock), Clock: clock}
	sessions := &websession.Manager{Store: sessionStore{now: now}, Clock: clock, PublicURL: "https://example.test"}
	h := &harness{web: New(Config{Tools: cfg, Sessions: sessions})}
	first := webCall(h, "POST", "/api/v1/read", `{"country":"vn","items":[{"handle":"vn:1","timeline":true}]}`)
	var result retrieval.ReadResult
	if first.Code != 200 || json.Unmarshal(first.Body.Bytes(), &result) != nil || len(result.Items) != 1 || result.Items[0].Next == "" {
		t.Fatalf("first page %d %s", first.Code, first.Body.String())
	}
	args := fmt.Sprintf(`{"country":"vn","release":%q,"items":[{"next":%q}]}`, result.Release, result.Items[0].Next)
	before := webCall(h, "POST", "/api/v1/read", args)
	store.active = "corpus_vn_20991111"
	lease, err := resolver.Resolve(t.Context(), "vn", "")
	if err != nil || lease.Release != "20991111" {
		t.Fatalf("activation: %+v, %v", lease, err)
	}
	lease.Done()
	after := webCall(h, "POST", "/api/v1/read", args)
	retained := after.Code == 200 && bytes.Equal(before.Body.Bytes(), after.Body.Bytes())
	expired := after.Code == 409 && bytes.Contains(after.Body.Bytes(), []byte(`"release_expired"`))
	if before.Code != 200 || (!retained && !expired) {
		t.Fatalf("continuation changed across activation: %d %s / %d %s", before.Code, before.Body.String(), after.Code, after.Body.String())
	}
}
