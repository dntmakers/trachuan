package build

import (
	"context"
	"encoding/json"
	"fmt"

	"danny.vn/trachuan/apps/corpus/internal/artifact"
	"danny.vn/trachuan/apps/corpus/internal/stage"
	"github.com/jackc/pgx/v5"
)

type detectorDatabaseQueryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func VerifyDetectorDatabase(ctx context.Context, workshop, release detectorDatabaseQueryer, artifacts artifact.Store, run stage.Run) (artifact.Result, error) {
	var path, hash, input, recipe, artifactStage, artifactKey, artifactInput, artifactRecipe string
	err := workshop.QueryRow(ctx, `SELECT a.path,a.sha256,i.input_hash,i.recipe_hash,a.stage,a.item_key,a.input_hash,a.recipe_hash FROM run_item i JOIN artifact a ON a.id=i.artifact_id WHERE i.run_id=$1 AND i.stage='build' AND i.item_key=$2 AND i.state='done'`, run.ID, run.Country+"/"+run.Rel).Scan(&path, &hash, &input, &recipe, &artifactStage, &artifactKey, &artifactInput, &artifactRecipe)
	if err != nil {
		return artifact.Result{}, fmt.Errorf("read committed build: %w", err)
	}
	if artifactStage != "build" || artifactKey != run.Country+"/"+run.Rel || artifactInput != input || artifactRecipe != recipe {
		return artifact.Result{}, fmt.Errorf("committed build artifact authority mismatch")
	}
	name, err := DatabaseName(run.Country, run.Rel)
	if err != nil {
		return artifact.Result{}, err
	}
	item := InventoryItem{ItemKey: run.Country + "/" + run.Rel, Path: &path, ArtifactSHA256: &hash}
	dir, err := artifactDir(artifacts, item)
	if err != nil {
		return artifact.Result{}, fmt.Errorf("verify committed build: %w", err)
	}
	data, err := artifact.ReadVerifiedFile(dir, "build.json", hash, 256<<20)
	if err != nil {
		return artifact.Result{}, fmt.Errorf("read committed build record: %w", err)
	}
	var record BuildRecord
	if err = json.Unmarshal(data, &record); err != nil {
		return artifact.Result{}, fmt.Errorf("decode committed build record: %w", err)
	}
	if record.Version != 1 || record.Country != run.Country || record.Rel != run.Rel || record.DatabaseName != name || record.BuildInputHash != input || record.BuildRecipeHash != recipe {
		return artifact.Result{}, fmt.Errorf("committed build record mismatch")
	}
	expected, ok := record.Counts["document"]
	if !ok || expected < 0 {
		return artifact.Result{}, fmt.Errorf("committed document count missing")
	}
	var marker *string
	if err = release.QueryRow(ctx, `SELECT shobj_description(oid, 'pg_database') FROM pg_database WHERE datname=current_database() AND datname=$1`, name).Scan(&marker); err != nil || marker == nil {
		return artifact.Result{}, fmt.Errorf("read database ownership marker")
	}
	parsed, err := parseOwnershipMarker(*marker)
	if err != nil || parsed.RunKey != run.Country+"/"+run.Rel || parsed.InputHash != input || parsed.RecipeHash != recipe {
		return artifact.Result{}, fmt.Errorf("database ownership marker mismatch")
	}
	matches, err := matchReleaseRecord(ctx, release, record, false)
	if err != nil || !matches {
		return artifact.Result{}, fmt.Errorf("database release identity mismatch")
	}
	var actual int64
	if err = release.QueryRow(ctx, `SELECT count(*) FROM document`).Scan(&actual); err != nil || actual != expected {
		return artifact.Result{}, fmt.Errorf("database document count mismatch")
	}
	return artifact.Result{Path: path, SHA256: hash, InputHash: input}, nil
}
