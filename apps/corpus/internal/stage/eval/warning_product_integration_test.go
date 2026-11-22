//go:build integration

package eval

import (
	"context"
	"testing"
	"time"

	"danny.vn/trachuan/apps/corpus/internal/evaluation"
	"danny.vn/trachuan/internal/retrieval"
)

func TestWarningEvaluation_ReviewedRelationThroughProduct(t *testing.T) {
	ctx := context.Background()
	pool, engine := warningProductFixture(t)
	c := evaluation.WarningCase{EntryID: "effect-example", Question: "ngân hàng khoản", Target: "vn:1#dieu-5", Acting: "vn:2#dieu-30", RelationKind: "repeals"}
	suite := warningIntegrationSuite(c)
	opts := evaluation.Options{EvaluationDate: retrieval.Date{Year: 2025, Month: time.January, Day: 1}, WarningResults: []evaluation.WarningResult{{EntryID: c.EntryID, Outcome: "applied", Search: []evaluation.WarningSearch{}}}, WarningScope: func(ctx context.Context, target, carrier string) (bool, error) {
		return warningScope(ctx, pool, target, carrier)
	}}
	for _, tc := range []struct {
		name, statement string
		want            bool
	}{
		{"reviewed", `INSERT INTO relation(id,source_document_id,target_document_id,source_provision_id,target_provision_id,source_key,target_key,source_number,target_number,kind,raw_code,raw_label,target_node_id,evidence,method,artifact_sha256,ordinal) VALUES (1,2,1,21,11,'vn:2','vn:1','n','n','repeals','','','dieu-5','exact','reviewed',repeat('a',64),1)`, true},
		{"different kind", `UPDATE relation SET kind='suspends' WHERE id=1`, false},
		{"absent", `DELETE FROM relation WHERE id=1`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := pool.Exec(ctx, tc.statement); err != nil {
				t.Fatal(err)
			}
			r, _, err := evaluation.Run(ctx, engine, suite, opts)
			if err != nil {
				t.Fatal(err)
			}
			if len(r.WarningCases) != 1 || len(r.WarningCases[0].Search) != 8 {
				t.Fatalf("warning records = %+v", r.WarningCases)
			}
			for _, search := range r.WarningCases[0].Search {
				if (search.Match != nil) != tc.want {
					t.Fatalf("search = %+v", search)
				}
			}
			if (r.WarningCases[0].Read != nil) != tc.want {
				t.Fatalf("read = %+v", r.WarningCases[0].Read)
			}
			if r.QualityPass != tc.want {
				t.Fatalf("quality = %v, want %v", r.QualityPass, tc.want)
			}
		})
	}
}
