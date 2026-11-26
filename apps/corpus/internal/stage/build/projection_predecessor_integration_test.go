//go:build integration

package build

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"danny.vn/trachuan/apps/corpus/internal/stage"
	"danny.vn/trachuan/apps/corpus/internal/stage/releasechange"
	"danny.vn/trachuan/apps/corpus/internal/store"
	"danny.vn/trachuan/internal/corpusdb"
	"danny.vn/trachuan/internal/releasechanges"
	"danny.vn/trachuan/internal/retrieval"
	"github.com/jackc/pgx/v5"
)

func prepareProjectionPredecessor(t *testing.T, f *loadFixture) {
	t.Helper()
	ctx := context.Background()
	rel := "20261118_3"
	recipe := retrieval.Recipe3()
	raw, err := recipe.JSON()
	if err != nil {
		t.Fatal(err)
	}
	report := fmt.Sprintf(`{"version":2,"scorer":"agent-eval/5","status":"complete","country":"vn","rel":%q,"retrieval_recipe":%s,"recipes":{"retrieval":%q}}`, rel, raw, recipe.Hash())
	reportHash := hashBytes([]byte(report))
	reportArt, err := f.artifacts.PublishContent(ctx, "previous-report", strings.Repeat("a", 64), func(dir string) error { return os.WriteFile(filepath.Join(dir, "report.json"), []byte(report), 0600) })
	if err != nil {
		t.Fatal(err)
	}
	f.run.Snapshot.PreviousReport = &stage.PreviousReport{SHA256: reportHash, Path: reportArt.Path}
	identity, err := os.ReadFile("../releasechange/testdata/identity.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	identity = []byte(strings.ReplaceAll(strings.ReplaceAll(string(identity), "20990102_1", rel), strings.Repeat("c", 64), reportHash))
	findings := fmt.Sprintf(`{"schema":2,"bindings":{"rel":%q,"report_sha256":"sha256:%s"},"findings":[]}`, rel, reportHash)
	art, err := f.artifacts.Publish(ctx, "evidence", strings.Repeat("b", 64), strings.Repeat("c", 64), func(dir string) error {
		if err := os.WriteFile(filepath.Join(dir, "identity.jsonl"), identity, 0600); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dir, "findings.json"), []byte(findings), 0600)
	})
	if err != nil {
		t.Fatal(err)
	}
	var id, artifactID int64
	if err = f.workshop.QueryRow(ctx, `INSERT INTO run(country,rel,state,snapshot) VALUES('vn',$1,'evaluated','{"PreviousReport":{"sha256":"none"}}') RETURNING id`, rel).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if err = f.workshop.QueryRow(ctx, `INSERT INTO artifact(stage,item_key,input_hash,recipe_hash,path,sha256) VALUES('evidence',$1,$2,$3,$4,$5) RETURNING id`, "vn/"+rel, strings.Repeat("b", 64), strings.Repeat("c", 64), art.Path, art.SHA256).Scan(&artifactID); err != nil {
		t.Fatal(err)
	}
	if _, err = f.workshop.Exec(ctx, `INSERT INTO run_item(run_id,stage,item_key,state,batch,artifact_id) VALUES($1,'evidence',$2,'done',0,$3)`, id, "vn/"+rel, artifactID); err != nil {
		t.Fatal(err)
	}
}

func TestBuildProjection_PredecessorMatchesSidecar(t *testing.T) {
	ctx := context.Background()
	f := recoverySetup(t, func(fixture *loadFixture) { prepareProjectionPredecessor(t, fixture) })
	cfg := f.admin.Config().Copy()
	cfg.Database = "corpus_tmp_" + f.run.Country + "_" + f.item.InputHash[:24]
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close(ctx) }()
	inv, err := LoadInventory(ctx, store.New(f.env.DB), f.run, "embed")
	if err != nil {
		t.Fatal(err)
	}
	members, err := releasechange.NewMembers(inv.Membership.Entries, inv.IdentityHash(), inv.IdentityHash())
	if err != nil {
		t.Fatal(err)
	}
	previous, err := releasechange.ResolvePredecessor(ctx, store.New(f.env.DB), f.env.Artifacts, f.run, *f.run.Snapshot.PreviousReport, f.run.Snapshot.LockedPredecessor.Release)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "identity.jsonl")
	current, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	bindings := releasechange.IdentityBindings{Rel: f.run.Rel}
	if err = releasechange.WriteIdentity(ctx, current, releasechange.NewSQLRelease(conn), members, bindings); err != nil {
		t.Fatal(err)
	}
	if err = current.Close(); err != nil {
		t.Fatal(err)
	}
	changesPath := filepath.Join(dir, "changes.jsonl")
	changes, err := os.Create(changesPath)
	if err != nil {
		t.Fatal(err)
	}
	changeBindings := releasechange.ChangesBindings{IdentityBindings: bindings, PredecessorRel: previous.Release, PredecessorIdentitySHA256: "sha256:" + previous.IdentitySHA256}
	if err = releasechange.WriteChanges(changes, releasechange.ChangeInputs{Current: path, Predecessor: previous.IdentityPath, Members: members, Bindings: changeBindings}); err != nil {
		t.Fatal(err)
	}
	if err = changes.Close(); err != nil {
		t.Fatal(err)
	}
	changes, err = os.Open(changesPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = changes.Close() }()
	header, err := ProjectionHeader(f.run)
	if err != nil {
		t.Fatal(err)
	}
	h, err := releasechanges.NewHasher(header)
	if err != nil {
		t.Fatal(err)
	}
	if err = releasechange.ProjectStream(changes, h.Add); err != nil {
		t.Fatal(err)
	}
	stored, err := corpusdb.ReadReleaseChanges(ctx, conn, corpusdb.ReleaseChangeLimits(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Digest != h.Sum() || !maps.Equal(stored.Counts, h.Counts()) || stored.Counts["document_added"] == 0 {
		b, _ := json.Marshal(stored.Counts)
		t.Fatalf("stored=%s sidecar=%s counts=%s", stored.Digest, h.Sum(), b)
	}
	record, _, err := verifyBuildArtifact(f.env.Artifacts, f.art, f.run, f.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = conn.Exec(ctx, `UPDATE release_change SET payload='{}' WHERE ordinal=1`); err != nil {
		t.Fatal(err)
	}
	if err = validateDatabase(ctx, f.cfg, cfg.Database, record); err == nil {
		t.Fatal("accepted altered projection row")
	}
}
