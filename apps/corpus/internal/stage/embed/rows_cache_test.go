package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"danny.vn/trachuan/apps/corpus/internal/stage/chunk"
	"danny.vn/trachuan/apps/corpus/internal/store"

	"danny.vn/trachuan/apps/corpus/internal/artifact"
	"danny.vn/trachuan/apps/corpus/internal/stage"
)

func TestRows_RejectsUnverifiedArtifact(t *testing.T) {
	ctx := context.Background()
	writer, err := New(nil, recipe()).Rows(ctx, stage.Env{Artifacts: artifact.Store{Root: t.TempDir()}}, artifact.Result{Path: "missing"})
	if err != nil {
		t.Fatal(err)
	}
	if err = writer(ctx, nil, 1); err == nil {
		t.Fatal("accepted unverified artifact")
	}
}

func TestRows_RejectsInvalidVectors(t *testing.T) {
	ctx := context.Background()
	s := New(nil, recipe())
	artifacts := artifact.Store{Root: t.TempDir()}
	file := chunk.File{DocumentKey: "doc", Chunks: []chunk.Chunk{{Content: "text"}}}
	art, _, _ := publishRepairBundle(t, s, artifacts, strings.Repeat("b", 64), file, func(_ *Index, vectors *[]byte) {
		*vectors = []byte("invalid")
	})
	writer, err := s.Rows(ctx, stage.Env{Artifacts: artifacts}, art)
	if err != nil {
		t.Fatal(err)
	}
	if err = writer(ctx, nil, 1); err == nil || !strings.Contains(err.Error(), "invalid vector size") {
		t.Fatalf("writer error = %v, want invalid vector size", err)
	}
}

type cacheQuery []store.LookupEmbedRowsRow

func (q cacheQuery) LookupEmbedRows(_ context.Context, _ store.LookupEmbedRowsParams) ([]store.LookupEmbedRowsRow, error) {
	return q, nil
}

func TestCache_ReusePreservesArtifactBytes(t *testing.T) {
	ctx := context.Background()
	chunks := []chunk.Chunk{{Ordinal: 0, Content: "a"}, {Ordinal: 1, Content: "b"}, {Ordinal: 2, Content: "c"}}
	file := chunk.File{Version: 1, DocumentKey: "doc", Chunks: chunks}
	coverage := chunk.Coverage{}
	input := strings.Repeat("b", 64)
	baselineFake := &fake{}
	s := New(baselineFake.client(recipe()), recipe())
	idx, vectors, err := s.build(ctx, file, coverage, input)
	if err != nil {
		t.Fatal(err)
	}
	artifacts := artifact.Store{Root: t.TempDir()}
	published, err := artifacts.Publish(ctx, "embed", input, s.Recipe(stage.Snapshot{}), func(dir string) error {
		data, e := json.Marshal(idx)
		if e != nil {
			return e
		}
		if e = os.WriteFile(filepath.Join(dir, "index.json"), data, 0600); e != nil {
			return e
		}
		data, e = json.Marshal(makeReport(idx, coverage))
		if e != nil {
			return e
		}
		if e = os.WriteFile(filepath.Join(dir, "report.json"), data, 0600); e != nil {
			return e
		}
		return os.WriteFile(filepath.Join(dir, "vectors.f32"), vectors, 0600)
	})
	if err != nil {
		t.Fatal(err)
	}
	entries := cacheQuery{}
	for _, row := range idx.Rows {
		entries = append(entries, store.LookupEmbedRowsRow{InputSha256: row.InputSHA256, Row: int32(row.Row), TokenCount: int32(row.TokenCount), ArtifactID: 1, ArtifactStage: "embed", ArtifactRecipeHash: s.Recipe(stage.Snapshot{}), InputHash: input, Path: published.Path, Sha256: published.SHA256})
	}
	env := stage.Env{Artifacts: artifacts}
	for _, tc := range []struct {
		name string
		rows cacheQuery
		want []string
	}{
		{"all", entries, nil}, {"partial", entries[:2], []string{"\nc"}},
		{"mismatch", cacheQuery{func() store.LookupEmbedRowsRow { r := entries[0]; r.InputSha256 = entries[1].InputSha256; return r }()}, []string{"\na", "\nb", "\nc"}},
		{"out of range", cacheQuery{func() store.LookupEmbedRowsRow { r := entries[0]; r.Row = 100; return r }()}, []string{"\na", "\nb", "\nc"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &fake{}
			subject := New(f.client(recipe()), recipe())
			hits, e := subject.lookupFrom(ctx, tc.rows, env, stage.Run{}, stage.Item{}, file)
			if e != nil {
				t.Fatal(e)
			}
			for _, hit := range hits {
				if cap(hit.bytes) != len(hit.bytes) {
					t.Fatalf("cached row retains vector buffer: len=%d cap=%d", len(hit.bytes), cap(hit.bytes))
				}
			}
			got, data, e := subject.buildCached(ctx, file, input, hits)
			if e != nil {
				t.Fatal(e)
			}
			wantIndex, _ := json.Marshal(idx)
			gotIndex, _ := json.Marshal(got)
			wantReport, _ := json.Marshal(makeReport(idx, coverage))
			gotReport, _ := json.Marshal(makeReport(got, coverage))
			if !bytes.Equal(wantIndex, gotIndex) || !bytes.Equal(vectors, data) || !bytes.Equal(wantReport, gotReport) {
				t.Fatal("cached artifact differs")
			}
			if tc.want == nil {
				if len(f.calls) != 0 {
					t.Fatalf("calls=%v", f.calls)
				}
			} else if len(f.bodies) != 2 || !reflect.DeepEqual(f.bodies[0], tc.want) || !reflect.DeepEqual(f.bodies[1], tc.want) {
				t.Fatalf("bodies=%v", f.bodies)
			}
		})
	}
	if err = os.WriteFile(filepath.Join(artifacts.Abs(published.Path), "vectors.f32"), []byte("bad"), 0600); err != nil {
		t.Fatal(err)
	}
	f := &fake{}
	subject := New(f.client(recipe()), recipe())
	hits, err := subject.lookupFrom(ctx, entries, env, stage.Run{}, stage.Item{}, file)
	if err != nil {
		t.Fatal(err)
	}
	got, data, err := subject.buildCached(ctx, file, input, hits)
	if err != nil {
		t.Fatal(err)
	}
	wantIndex, _ := json.Marshal(idx)
	gotIndex, _ := json.Marshal(got)
	if len(hits) != 0 || len(f.calls) != 2 || !bytes.Equal(wantIndex, gotIndex) || !bytes.Equal(vectors, data) {
		t.Fatal("invalid artifact did not re-embed")
	}
}

func TestCache_FakeProducesDistinctRows(t *testing.T) {
	f := &fake{counts: map[string]int{"\na": 2, "\nb": 3}}
	idx, vectors, err := run(t, f, chunk.Chunk{Ordinal: 0, Content: "a"}, chunk.Chunk{Ordinal: 1, Content: "b"})
	if err != nil {
		t.Fatal(err)
	}
	if idx.Rows[0].TokenCount == idx.Rows[1].TokenCount || bytes.Equal(vectors[:8], vectors[8:16]) {
		t.Fatal("fake rows must differ by input")
	}
}

func TestCache_InterleavedHitsAndBatchedMissesPreserveBytes(t *testing.T) {
	ctx := context.Background()
	r := recipe()
	r.TokenCap = 64
	counts := make(map[string]int)
	chunks := make([]chunk.Chunk, 42)
	for i := range chunks {
		content := fmt.Sprintf("text-%02d", i)
		chunks[i] = chunk.Chunk{Ordinal: i, Content: content}
		counts["\n"+content] = i + 1
	}
	counts["\ntext-20"] = 65
	chunks[41].Content = strings.Repeat("x", 1048576)
	file := chunk.File{Version: 1, DocumentKey: "doc", Chunks: chunks}
	coverage := chunk.Coverage{}
	input := strings.Repeat("b", 64)
	f := &fake{counts: counts}
	subject := New(f.client(r), r)
	idx, vectors, err := subject.build(ctx, file, coverage, input)
	if err != nil {
		t.Fatal(err)
	}
	if len(idx.Rejected) != 2 || idx.Rejected[0].Kind != "embedding_input_too_long" || idx.Rejected[1].Kind != "embedding_input_bytes_exceeded" {
		t.Fatalf("rejected=%+v", idx.Rejected)
	}
	hits := make(map[string]cachedRow)
	for _, row := range idx.Rows {
		if row.ChunkOrdinal%4 == 0 {
			start := row.Row * 8
			hits[row.InputSHA256] = cachedRow{bytes: append([]byte(nil), vectors[start:start+8]...), count: row.TokenCount}
		}
	}
	mixed := &fake{counts: counts}
	got, data, err := New(mixed.client(r), r).buildCached(ctx, file, input, hits)
	if err != nil {
		t.Fatal(err)
	}
	wantIndex, _ := json.Marshal(idx)
	gotIndex, _ := json.Marshal(got)
	wantReport, _ := json.Marshal(makeReport(idx, coverage))
	gotReport, _ := json.Marshal(makeReport(got, coverage))
	if !bytes.Equal(wantIndex, gotIndex) || !bytes.Equal(vectors, data) || !bytes.Equal(wantReport, gotReport) {
		t.Fatal("mixed cache output differs from fresh output")
	}
	if len(mixed.bodies) < 4 || len(mixed.bodies[0]) != 16 {
		t.Fatalf("miss batches=%v", mixed.bodies)
	}
	for _, body := range mixed.bodies {
		for _, text := range body {
			if _, ok := hits[hashText(text)]; ok {
				t.Fatal("cached input sent to client")
			}
		}
	}
}

func TestCache_CopiedRowDoesNotAliasSource(t *testing.T) {
	a := cachedArtifact{index: Index{Dimensions: 2}, vectors: []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}}
	got := copyCachedRow(a, store.LookupEmbedRowsRow{Row: 1, TokenCount: 3})
	a.vectors[8] = 99
	if got.bytes[0] != 9 || got.count != 3 {
		t.Fatalf("copied row=%v count=%d", got.bytes, got.count)
	}
}

func TestResolve_RejectsWrongIndexIdentity(t *testing.T) {
	for _, tc := range []struct {
		name  string
		alter func(*Index, *[]byte)
	}{
		{"recipe", func(idx *Index, _ *[]byte) {
			idx.Recipe.TokenCap++
			idx.RecipeHash = idx.Recipe.Hash()
		}},
		{"chunk hash", func(idx *Index, _ *[]byte) {
			idx.ChunkSHA256 = strings.Repeat("c", 64)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := New(nil, recipe())
			artifacts := artifact.Store{Root: t.TempDir()}
			file := chunk.File{DocumentKey: "doc", Chunks: []chunk.Chunk{{Content: "text"}}}
			art, _, _ := publishRepairBundle(t, s, artifacts, strings.Repeat("b", 64), file, tc.alter)
			if _, err := s.Resolve(context.Background(), stage.Env{Artifacts: artifacts}, stage.Run{}, stage.Item{}, art); err == nil || !strings.Contains(err.Error(), "embed artifact identity mismatch") {
				t.Fatalf("resolve error = %v, want embed artifact identity mismatch", err)
			}
		})
	}
}

func TestOpenRecorded_ComparesResolvedPaths(t *testing.T) {
	s := New(nil, recipe())
	root := t.TempDir()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	rel, err := filepath.Rel(cwd, root)
	if err != nil {
		t.Fatal(err)
	}
	artifacts := artifact.Store{Root: root}
	file := chunk.File{DocumentKey: "doc", Chunks: []chunk.Chunk{{Content: "text"}}}
	art, _, _ := publishRepairBundle(t, s, artifacts, strings.Repeat("b", 64), file, nil)
	absPath := filepath.Join(root, art.Path)
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(absPath, alias); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, root, record string
		stale              bool
	}{
		{"relative root and absolute record", rel, absPath, false},
		{"absolute root and relative record", root, art.Path, false},
		{"root-prefixed legacy record", rel, filepath.Join(rel, art.Path), false},
		{"different path", rel, filepath.Join(root, "other"), true},
		{"symlink spelling", rel, alias, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			record := art
			record.Path = tc.record
			got := s.openRecorded(artifact.Store{Root: tc.root}, s.Recipe(stage.Snapshot{}), record)
			if got.stale != tc.stale || (got.err != nil) != tc.stale {
				t.Fatalf("open recorded stale = %t, error = %v; want stale %t", got.stale, got.err, tc.stale)
			}
		})
	}
}
