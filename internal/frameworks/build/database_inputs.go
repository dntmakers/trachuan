package build

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"

	"danny.vn/trachuan/internal/frameworks/parse"

	"danny.vn/trachuan/internal/frameworks/registry"
	"danny.vn/trachuan/internal/frameworks/relate"
)

type stepProvenance struct {
	Recipe    recipe
	InputHash string
}

type databaseInputs struct {
	parserTable parse.Table
	versions    []registry.Framework
	controls    []Control
	chunks      []Chunk
	edges       []relate.Edge
	gaps        []Gap
	steps       map[string]stepProvenance
	cases       []GoldenCase
	caseHash    string
	state       *runState
}

func readDatabaseInputs(base string, snapshots ...*snapshotParseInputs) (databaseInputs, error) {
	var d databaseInputs
	frozen, err := snapshotForStep(base, snapshots...)
	if err != nil {
		return d, err
	}
	d.parserTable = frozen.table
	d.state = frozen.artifactState()
	reg := frozen.registry
	policy, err := frozen.read("gap-kinds.json", false)
	if err != nil {
		return d, err
	}
	expected, err := json.Marshal(gapKinds())
	if err != nil {
		return d, hide("encode gap kind table", err)
	}
	if !equalJSON(policy, expected) {
		return d, fmt.Errorf("gap kind table changed")
	}
	d.versions = releaseVersions(reg)
	if d.controls, err = readStepRows[Control](frozen, base, "parse", "controls.jsonl"); err != nil {
		return d, err
	}
	caseData, err := frozen.read("trachuan/golden.csv", true)
	if err != nil {
		return d, err
	}
	review, err := frozen.read("trachuan/golden-review.yaml", true)
	if err != nil {
		return d, err
	}
	if err = validateGoldenReviewData(caseData, review); err != nil {
		return d, err
	}
	if caseData != nil {
		d.cases, err = MapGoldenCases(bytes.NewReader(caseData), reg, d.controls)
		if err != nil {
			return d, err
		}
		d.caseHash = digest(caseData)
	}

	if d.chunks, err = readStepRows[Chunk](frozen, base, "chunk", "chunks.jsonl"); err != nil {
		return d, err
	}
	if d.edges, err = readStepRows[relate.Edge](frozen, base, "relate", "edges.jsonl"); err != nil {
		return d, err
	}
	d.steps = map[string]stepProvenance{}
	access := map[string]string{}
	for _, v := range d.versions {
		access[v.Code+":"+v.Version] = v.Access
	}
	seen := map[string]bool{}
	for _, step := range []string{"snapshot", "parse", "relate", "chunk", "embed"} {
		var raw []byte
		if step == "snapshot" {
			raw = frozen.manifestData
		} else {
			var stepArtifact *frozenStep
			stepArtifact, err = d.state.capture(base, step)
			if err == nil {
				raw = stepArtifact.raw
			}
		}
		if err != nil {
			return d, err
		}
		var m manifest
		if err = json.Unmarshal(raw, &m); err != nil {
			return d, hide("decode step provenance", err)
		}
		d.steps[step] = stepProvenance{m.Recipe, stepInputHash(m.Inputs)}
		var gaps []Gap
		if step == "snapshot" {
			raw, err = frozen.read("gaps.jsonl", false)
			if err == nil {
				gaps, err = decodeRows[Gap](raw)
			}
		} else {
			gaps, err = readStepRows[Gap](frozen, base, step, "gaps.jsonl")
		}
		if err != nil {
			return d, err
		}
		for _, g := range gaps {
			if _, _, err = classifyGap(g.Kind, access[g.Code+":"+g.Version]); err != nil {
				return d, err
			}
			if g.Count <= 0 {
				return d, fmt.Errorf("invalid gap count")
			}
			key, _ := json.Marshal([]string{g.Kind, g.Code, g.Version, gapSubject(g), strconv.Itoa(g.Count)})
			if seen[string(key)] {
				return d, fmt.Errorf("duplicate gap key")
			}
			seen[string(key)] = true
			d.gaps = append(d.gaps, g)
		}
	}
	return d, nil
}

func gapSubject(g Gap) string {
	if g.Citation != nil {
		return *g.Citation
	}
	return "fw:" + g.Code + ":" + g.Version
}
