package build

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"danny.vn/trachuan/apps/corpus/internal/artifact"
	"danny.vn/trachuan/apps/corpus/internal/stage"
	"github.com/jackc/pgx/v5"
)

type detectorRow func(...any) error

func (r detectorRow) Scan(dest ...any) error { return r(dest...) }

type detectorQuery func(context.Context, string, ...any) pgx.Row

func (q detectorQuery) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return q(ctx, sql, args...)
}

func TestVerifyDetectorDatabaseRejectsUntrustedRelease(t *testing.T) {
	ctx := context.Background()
	input, recipe := strings.Repeat("a", 64), strings.Repeat("b", 64)
	run := stage.Run{ID: 1, Country: "vn", Rel: "20990101"}
	name, _ := DatabaseName(run.Country, run.Rel)
	record := BuildRecord{Version: 1, Country: run.Country, Rel: run.Rel, DatabaseName: name, BuildInputHash: input, BuildRecipeHash: recipe, Counts: map[string]int64{"document": 2}}
	data, _ := json.Marshal(record)
	store := artifact.Store{Root: t.TempDir()}
	art, err := store.Publish(ctx, "build", input, recipe, func(dir string) error { return os.WriteFile(filepath.Join(dir, "build.json"), data, 0600) })
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"valid", "missing marker", "foreign marker", "release mismatch", "fewer", "more", "item recipe", "missing item", "corrupt record"} {
		t.Run(scenario, func(t *testing.T) {
			itemRecipe := recipe
			if scenario == "item recipe" {
				itemRecipe = strings.Repeat("c", 64)
			}
			workshop := detectorQuery(func(_ context.Context, _ string, _ ...any) pgx.Row {
				return detectorRow(func(dest ...any) error {
					if scenario == "missing item" {
						return pgx.ErrNoRows
					}
					values := []string{art.Path, art.SHA256, input, itemRecipe, "build", run.Country + "/" + run.Rel, input, recipe}
					for i, v := range values {
						*dest[i].(*string) = v
					}
					return nil
				})
			})
			release := detectorQuery(func(_ context.Context, sql string, _ ...any) pgx.Row {
				return detectorRow(func(dest ...any) error {
					switch {
					case strings.Contains(sql, "shobj_description"):
						marker := fmt.Sprintf("%s/%s:%s:%s", run.Country, run.Rel, input, recipe)
						if scenario == "missing marker" {
							marker = ""
						}
						if scenario == "foreign marker" {
							marker = fmt.Sprintf("%s/%s:%s:%s", run.Country, run.Rel, strings.Repeat("d", 64), recipe)
						}
						*dest[0].(**string) = &marker
					case strings.Contains(sql, "FROM release"):
						*dest[0].(*bool) = scenario != "release mismatch"
					case strings.Contains(sql, "count(*)"):
						value := int64(2)
						if scenario == "fewer" {
							value = 1
						}
						if scenario == "more" {
							value = 3
						}
						*dest[0].(*int64) = value
					default:
						t.Fatalf("unexpected SQL %s", sql)
					}
					return nil
				})
			})
			if scenario == "corrupt record" {
				if e := os.WriteFile(filepath.Join(store.Abs(art.Path), "build.json"), []byte("{}"), 0600); e != nil {
					t.Fatal(e)
				}
				defer func() {
					if e := os.WriteFile(filepath.Join(store.Abs(art.Path), "build.json"), data, 0600); e != nil {
						t.Error(e)
					}
				}()
			}
			_, err := VerifyDetectorDatabase(ctx, workshop, release, store, run)
			if (err == nil) != (scenario == "valid") {
				t.Fatalf("scenario=%s error=%v", scenario, err)
			}
		})
	}
}
