package build

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
)

type EmbedRejection struct {
	Ordinal    int
	TokenCount *int
	Kind       string
}

type EmbedResult struct {
	Index    json.RawMessage
	Vectors  []byte
	Rows     int
	Rejected []EmbedRejection
}

type EmbedArtifact struct{ Index, Vectors []byte }

type Embedder interface {
	Recipe() string
	Validate(context.Context, EmbedArtifact, string, []Chunk, string) error
	Check(context.Context) error
	Build(context.Context, string, []Chunk, string) (EmbedResult, error)
	Read(context.Context, EmbedArtifact, string, []Chunk, string) ([]Vector, error)
}

func embedStep(ctx context.Context, base, dir string, service Embedder, snapshots ...*snapshotParseInputs) error {
	if service == nil {
		return fmt.Errorf("embedder unavailable")
	}
	if err := service.Check(ctx); err != nil {
		return err
	}
	frozen, err := snapshotForStep(base, snapshots...)
	if err != nil {
		return err
	}
	chunk, err := frozen.artifactState().capture(base, "chunk")
	if err != nil {
		return err
	}
	chunks, err := readStepRows[Chunk](frozen, base, "chunk", "chunks.jsonl")
	if err != nil {
		return err
	}
	reg := frozen.registry
	data := chunk.raw
	type identity struct{ code, version string }
	identities := map[string]identity{}
	groups := map[string][]Chunk{}
	for _, version := range releaseVersions(reg) {
		for _, prefix := range []string{"fw:", "fwp:"} {
			key := prefix + version.Code + ":" + version.Version
			identities[key] = identity{version.Code, version.Version}
			groups[key] = nil
		}
	}
	for _, c := range chunks {
		if _, ok := identities[c.DocumentKey]; !ok {
			return fmt.Errorf("unknown document key")
		}
		groups[c.DocumentKey] = append(groups[c.DocumentKey], c)
	}
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	type gapKey struct{ kind, code, version, citation string }
	counts := map[gapKey]int{}
	for _, key := range keys {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("embed document: %w", err)
		}
		result, err := service.Build(ctx, key, groups[key], digest(data))
		if err != nil {
			return fmt.Errorf("embed document %s: %w", key, err)
		}
		byOrdinal := make(map[int]Chunk, len(groups[key]))
		for _, c := range groups[key] {
			byOrdinal[c.Ordinal] = c
		}
		reportGaps := make([]reportGap, 0, len(result.Rejected))
		for _, reject := range result.Rejected {
			c, ok := byOrdinal[reject.Ordinal]
			if !ok {
				return fmt.Errorf("invalid rejected ordinal")
			}
			detail := fmt.Sprintf("chunk ordinal %d", reject.Ordinal)
			if reject.TokenCount != nil {
				detail += fmt.Sprintf(", token count %d", *reject.TokenCount)
			}
			reportGaps = append(reportGaps, reportGap{reject.Kind, detail})
			id := identities[key]
			counts[gapKey{reject.Kind, id.code, id.version, c.Citation}]++
		}
		report := embedReport{1, result.Rows, len(result.Rejected), reportGaps}
		artifact := filepath.Join("artifacts", digest([]byte(key)))
		if err := writeEmbedArtifact(dir, artifact, result, report, frozen.stage); err != nil {
			return err
		}
	}
	var gaps []Gap
	for key, count := range counts {
		citation := key.citation
		gaps = append(gaps, Gap{Kind: key.kind, Code: key.code, Version: key.version, Citation: &citation, Count: count})
	}
	return writeGaps(dir, gaps, frozen.stage)
}

type reportGap struct {
	Kind   string `json:"kind"`
	Detail string `json:"detail"`
}
type embedReport struct {
	Version  int         `json:"version"`
	Rows     int         `json:"rows"`
	Rejected int         `json:"rejected"`
	Gaps     []reportGap `json:"gaps"`
}

func writeEmbedArtifact(dir, artifact string, result EmbedResult, report embedReport, stage *stagedOutputs) error {
	reportData, err := json.Marshal(report)
	if err != nil {
		return hide("encode report", err)
	}
	files := map[string][]byte{"index.json": result.Index, "report.json": reportData, "vectors.f32": result.Vectors}
	lines := make([]string, 0, len(files))
	for name, data := range files {
		if err := writeFile(dir, filepath.Join(artifact, name), data, stage); err != nil {
			return err
		}
		lines = append(lines, fmt.Sprintf("%s  %s\n", digest(data), name))
	}
	sort.Strings(lines)
	var manifest []byte
	for _, line := range lines {
		manifest = append(manifest, line...)
	}
	return writeFile(dir, filepath.Join(artifact, "MANIFEST.sha256"), manifest, stage)
}
