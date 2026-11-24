package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"danny.vn/trachuan/apps/corpus/internal/artifact"
	"danny.vn/trachuan/apps/corpus/internal/stage"
	"danny.vn/trachuan/apps/corpus/internal/stage/chunk"
	"danny.vn/trachuan/apps/corpus/internal/store"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type repairKey struct{ recipe, hash string }
type repairLocator struct {
	id         int64
	row, count int32
}

type repairDB struct {
	pgx.Tx
	artifacts map[int64]artifact.Result
	recipe    string
	rows      map[repairKey]repairLocator
	beforeCAS func()
	lookupErr error
}

type repairScan []any

func (row repairScan) Scan(dest ...any) error {
	if len(row) != len(dest) {
		return fmt.Errorf("wrong scan length: %d != %d", len(row), len(dest))
	}
	for i, value := range row {
		reflect.ValueOf(dest[i]).Elem().Set(reflect.ValueOf(value))
	}
	return nil
}

type repairRows struct {
	pgx.Rows
	records []repairScan
	index   int
}

func (*repairRows) Close()     {}
func (*repairRows) Err() error { return nil }
func (r *repairRows) Next() bool {
	r.index++
	return r.index <= len(r.records)
}
func (r *repairRows) Scan(dest ...any) error { return r.records[r.index-1].Scan(dest...) }

func (db *repairDB) QueryRow(_ context.Context, query string, args ...any) pgx.Row {
	if !strings.Contains(query, "name: MatchEmbedArtifact ") {
		panic(query)
	}
	a, ok := db.artifacts[args[0].(int64)]
	return repairScan{ok && db.recipe == args[1] && a.InputHash == args[2] && a.Path == args[3] && a.SHA256 == args[4]}
}

func (db *repairDB) Begin(context.Context) (pgx.Tx, error) { return db, nil }
func (*repairDB) Commit(context.Context) error             { return nil }

func (db *repairDB) Query(_ context.Context, query string, args ...any) (pgx.Rows, error) {
	result := &repairRows{}
	if strings.Contains(query, "name: ListEmbedArtifacts ") {
		if args[0] != db.recipe {
			return result, nil
		}
		ids := make([]int64, 0, len(db.artifacts))
		for id := range db.artifacts {
			ids = append(ids, id)
		}
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		for _, id := range ids {
			a := db.artifacts[id]
			result.records = append(result.records, repairScan{id, a.InputHash, a.Path, a.SHA256})
		}
		return result, nil
	}
	if !strings.Contains(query, "name: LookupEmbedRows ") {
		return nil, fmt.Errorf("unexpected query: %s", query)
	}
	if db.lookupErr != nil {
		return nil, db.lookupErr
	}
	for _, hash := range args[1].([]string) {
		if loc, ok := db.rows[repairKey{args[0].(string), hash}]; ok {
			a := db.artifacts[loc.id]
			result.records = append(result.records, repairScan{hash, loc.row, loc.count, loc.id, a.InputHash, a.Path, a.SHA256, "embed", db.recipe})
		}
	}
	return result, nil
}

func (db *repairDB) Exec(_ context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	switch {
	case strings.Contains(query, "name: InsertEmbedRow "):
		key := repairKey{args[0].(string), args[1].(string)}
		if _, exists := db.rows[key]; exists {
			return pgconn.NewCommandTag("INSERT 0 0"), nil
		}
		db.rows[key] = repairLocator{args[2].(int64), args[3].(int32), args[4].(int32)}
		return pgconn.NewCommandTag("INSERT 0 1"), nil
	case strings.Contains(query, "name: RepairEmbedRow "):
		key := repairKey{args[3].(string), args[4].(string)}
		if db.beforeCAS != nil {
			db.beforeCAS()
		}
		old := repairLocator{args[5].(int64), args[6].(int32), args[7].(int32)}
		if db.rows[key] != old {
			return pgconn.NewCommandTag("UPDATE 0"), nil
		}
		db.rows[key] = repairLocator{args[0].(int64), args[1].(int32), args[2].(int32)}
		return pgconn.NewCommandTag("UPDATE 1"), nil
	default:
		return pgconn.CommandTag{}, fmt.Errorf("unexpected write: %s", query)
	}
}

func publishRepairBundle(t *testing.T, s *Stage, artifacts artifact.Store, input string, file chunk.File, alter func(*Index, *[]byte)) (artifact.Result, Index, []byte) {
	t.Helper()
	ctx := context.Background()
	idx, vectors, err := New((&fake{}).client(s.expected), s.expected).build(ctx, file, chunk.Coverage{}, input)
	if err != nil {
		t.Fatal(err)
	}
	if alter != nil {
		alter(&idx, &vectors)
	}
	art, err := artifacts.Publish(ctx, "embed", input, s.Recipe(stage.Snapshot{}), func(dir string) error {
		for name, value := range map[string]any{"index.json": idx, "report.json": makeReport(idx, chunk.Coverage{})} {
			data, encodeErr := json.Marshal(value)
			if encodeErr != nil {
				return encodeErr
			}
			if writeErr := os.WriteFile(filepath.Join(dir, name), data, 0600); writeErr != nil {
				return writeErr
			}
		}
		return os.WriteFile(filepath.Join(dir, "vectors.f32"), vectors, 0600)
	})
	if err != nil {
		t.Fatal(err)
	}
	return art, idx, vectors
}

func TestRows_RepairsObservedStalePointer(t *testing.T) {
	ctx := context.Background()
	s := New(nil, recipe())
	artifacts := artifact.Store{Root: t.TempDir()}
	env := stage.Env{Artifacts: artifacts}
	file := chunk.File{DocumentKey: "doc", Chunks: []chunk.Chunk{{Ordinal: 1, Content: "shared"}}}
	candidate, index, vectors := publishRepairBundle(t, s, artifacts, strings.Repeat("a", 64), file, nil)
	incumbent, _, _ := publishRepairBundle(t, s, artifacts, strings.Repeat("b", 64), file, nil)
	if err := os.RemoveAll(artifacts.Abs(incumbent.Path)); err != nil {
		t.Fatal(err)
	}
	key := repairKey{s.Recipe(stage.Snapshot{}), index.Rows[0].InputSHA256}
	otherRecipe := repairKey{"another recipe", key.hash}
	initial := repairLocator{2, 0, int32(index.Rows[0].TokenCount)}
	db := &repairDB{recipe: key.recipe, artifacts: map[int64]artifact.Result{1: candidate, 2: incumbent}, rows: map[repairKey]repairLocator{key: initial, otherRecipe: initial}}
	writer, err := s.Rows(ctx, env, candidate)
	if err != nil {
		t.Fatal(err)
	}
	if err = writer(ctx, db, 1); err != nil {
		t.Fatal(err)
	}
	if got := db.rows[key]; got != (repairLocator{1, 0, initial.count}) {
		t.Fatalf("incumbent after repair = %+v, want older candidate 1", got)
	}
	if db.rows[otherRecipe] != initial {
		t.Fatal("repair changed a different recipe")
	}
	assertRepairReuse(t, s, env, db, file, candidate.InputHash, vectors)
}

func assertRepairReuse(t *testing.T, s *Stage, env stage.Env, db *repairDB, file chunk.File, hash string, want []byte) {
	t.Helper()
	ctx := context.Background()
	hits, err := s.lookupFrom(ctx, store.New(db), env, stage.Run{}, stage.Item{}, file)
	if err != nil || len(hits) != len(file.Chunks) {
		t.Fatalf("repaired cache hits = %d, err = %v", len(hits), err)
	}
	_, got, err := s.buildCached(ctx, file, hash, hits)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("cache-only vectors = %v, err = %v", got, err)
	}
}

func TestBackfill_RepairsOnceAndSkipsBadCandidates(t *testing.T) {
	ctx := context.Background()
	s := New(nil, recipe())
	artifacts := artifact.Store{Root: t.TempDir()}
	file := chunk.File{DocumentKey: "doc", Chunks: []chunk.Chunk{{Content: "shared"}, {Ordinal: 1, Content: "new"}}}
	bad, _, _ := publishRepairBundle(t, s, artifacts, strings.Repeat("a", 64), file, func(_ *Index, vectors *[]byte) {
		*vectors = []byte("bad vector size")
	})
	wrong, _, _ := publishRepairBundle(t, s, artifacts, strings.Repeat("b", 64), file, func(idx *Index, _ *[]byte) {
		idx.ChunkSHA256 = strings.Repeat("e", 64)
	})
	candidate, idx, vectors := publishRepairBundle(t, s, artifacts, strings.Repeat("c", 64), file, nil)
	missing, _, _ := publishRepairBundle(t, s, artifacts, strings.Repeat("d", 64), file, nil)
	if err := os.RemoveAll(artifacts.Abs(missing.Path)); err != nil {
		t.Fatal(err)
	}
	recipeHash := s.Recipe(stage.Snapshot{})
	key := repairKey{recipeHash, idx.Rows[0].InputSHA256}
	db := &repairDB{recipe: recipeHash, artifacts: map[int64]artifact.Result{1: bad, 2: wrong, 3: candidate, 4: missing}, rows: map[repairKey]repairLocator{key: {4, 0, 2}}}
	for _, want := range []string{
		"artifacts verified=1 rows inserted=1 rows repaired=1 artifacts skipped=3",
		"artifacts verified=1 rows inserted=0 rows repaired=0 artifacts skipped=3",
	} {
		var out bytes.Buffer
		if err := s.backfill(ctx, artifacts, db, recipeHash, func(row store.LookupEmbedRowsRow) cachedArtifact {
			return s.openCached(artifacts, recipeHash, row)
		}, &out); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), want) {
			t.Fatalf("backfill output = %q, want %q", out.String(), want)
		}
	}
	if got := db.rows[key]; got.id != 3 {
		t.Fatalf("indexed locator = %+v, want candidate 3", got)
	}
	assertRepairReuse(t, s, stage.Env{Artifacts: artifacts}, db, file, candidate.InputHash, vectors)
}

func TestRows_DoesNotClobberChangedOrUnprovenPointer(t *testing.T) {
	for _, name := range []string{"healthy", "concurrent token count change", "unreadable incumbent", "lookup failure", "wrong candidate ID"} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			s := New(nil, recipe())
			artifacts := artifact.Store{Root: t.TempDir()}
			file := chunk.File{DocumentKey: "doc", Chunks: []chunk.Chunk{{Content: "shared"}}}
			candidate, idx, _ := publishRepairBundle(t, s, artifacts, strings.Repeat("a", 64), file, nil)
			incumbent, _, _ := publishRepairBundle(t, s, artifacts, strings.Repeat("b", 64), file, nil)
			recipeHash := s.Recipe(stage.Snapshot{})
			key := repairKey{recipeHash, idx.Rows[0].InputSHA256}
			want := repairLocator{2, 0, int32(idx.Rows[0].TokenCount)}
			db := &repairDB{recipe: recipeHash, artifacts: map[int64]artifact.Result{1: candidate, 2: incumbent}, rows: map[repairKey]repairLocator{key: want}}
			id := int64(1)
			switch name {
			case "concurrent token count change":
				wrong := want
				wrong.count++
				db.rows[key] = wrong
				db.beforeCAS = func() { db.rows[key] = want }
			case "unreadable incumbent":
				path := artifacts.Abs(incumbent.Path)
				if err := os.RemoveAll(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Base(path), path); err != nil {
					t.Fatal(err)
				}
				if _, _, err := artifacts.OpenRecorded("embed", incumbent.InputHash, recipeHash, incumbent.SHA256); err == nil {
					t.Fatal("fixture does not produce an unreadable artifact")
				}
			case "lookup failure":
				db.lookupErr = fmt.Errorf("database unavailable")
			case "wrong candidate ID":
				id = 2
			}
			writer, err := s.Rows(ctx, stage.Env{Artifacts: artifacts}, candidate)
			if err != nil {
				t.Fatal(err)
			}
			err = writer(ctx, db, id)
			if wantError := name == "wrong candidate ID" || name == "lookup failure"; (err != nil) != wantError {
				t.Fatalf("index error: %v, want error: %t", err, wantError)
			}
			if got := db.rows[key]; got != want {
				t.Fatalf("incumbent after indexing = %+v, want %+v", got, want)
			}
		})
	}
}

func TestBackfill_OpensSharedIncumbentOnce(t *testing.T) {
	ctx := context.Background()
	s := New(nil, recipe())
	artifacts := artifact.Store{Root: t.TempDir()}
	file := chunk.File{DocumentKey: "doc", Chunks: []chunk.Chunk{{Content: "shared"}}}
	first, idx, _ := publishRepairBundle(t, s, artifacts, strings.Repeat("a", 64), file, nil)
	second, _, _ := publishRepairBundle(t, s, artifacts, strings.Repeat("b", 64), file, nil)
	file.Chunks = append(file.Chunks, chunk.Chunk{Ordinal: 1, Content: "extra incumbent row"})
	incumbent, _, _ := publishRepairBundle(t, s, artifacts, strings.Repeat("c", 64), file, nil)
	recipeHash := s.Recipe(stage.Snapshot{})
	key := repairKey{recipeHash, idx.Rows[0].InputSHA256}
	locator := repairLocator{3, 0, int32(idx.Rows[0].TokenCount)}
	db := &repairDB{recipe: recipeHash, artifacts: map[int64]artifact.Result{1: first, 2: second, 3: incumbent}, rows: map[repairKey]repairLocator{key: locator}}
	opens := 0
	var out bytes.Buffer
	if err := s.backfill(ctx, artifacts, db, recipeHash, func(row store.LookupEmbedRowsRow) cachedArtifact {
		opens++
		return s.openCached(artifacts, recipeHash, row)
	}, &out); err != nil {
		t.Fatal(err)
	}
	if opens != 1 || db.rows[key] != locator {
		t.Fatalf("shared incumbent opened %d times, row = %+v; want one open and unchanged row", opens, db.rows[key])
	}
}
