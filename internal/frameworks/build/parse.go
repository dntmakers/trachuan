package build

import (
	"context"
	"fmt"
	"sort"

	"danny.vn/trachuan/internal/frameworks/parse"
	"danny.vn/trachuan/internal/frameworks/relate"
)

type catalog struct {
	Code    string       `json:"code"`
	Version string       `json:"version"`
	Nodes   []parse.Node `json:"nodes"`
}

func parseStep(ctx context.Context, base, dir string) error {
	frozen, err := readSnapshotParseInputs(base, nil)
	if err != nil {
		return err
	}
	if err = frozen.loadParseInputs(); err != nil {
		return err
	}
	return parseStepWith(ctx, base, dir, nil, nil, frozen)
}

func parseStepWith(ctx context.Context, base, dir string, extractor PDFExtractor, table parse.Table, frozen *snapshotParseInputs) error {
	if frozen == nil {
		return fmt.Errorf("snapshot inputs missing")
	}
	reg := frozen.registry
	titles := map[relate.Key]string{}
	if frozen.titles != nil {
		var err error
		titles, err = parseTitles(frozen.titles)
		if err != nil {
			return err
		}
	}
	var controls []Control
	var catalogs []catalog
	var gaps []Gap
	versions := releaseVersions(reg)
	titleCatalog := newTitleCatalog()
	for _, v := range versions {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		file, selectErr := v.SelectedSourceWithTable(table)
		if selectErr != nil {
			return hide("select source", selectErr)
		}
		data, ok := frozen.sources[file.Path]
		if !ok {
			return fmt.Errorf("snapshot source missing")
		}
		result, parseErr := ParseSourceWithTable(ctx, v, data, extractor, table)
		if parseErr != nil {
			return parseErr
		}
		if lineageErr := validateLineage(result.Nodes); lineageErr != nil {
			return lineageErr
		}
		catalogs = append(catalogs, catalog{v.Code, v.Version, result.Nodes})
		titleCatalog.add(v, result.Nodes)
	}
	titles, err := titleCatalog.validate(titles, false)
	if err != nil {
		return err
	}
	for i, v := range versions {
		result := &parse.Result{Nodes: catalogs[i].Nodes}
		var rows []Control
		for _, n := range result.Nodes {
			row := Control{Code: v.Code, Version: v.Version, Citation: n.Citation, CitationNorm: n.CitationNorm, Kind: n.Kind, Ordinal: n.Ordinal, Status: n.Status, TitleOriginal: n.PublisherTitle, Body: n.Body, Notes: []string{}, Tables: []string{}}
			if n.Parent >= 0 {
				parent := result.Nodes[n.Parent].Citation
				row.Parent = &parent
			}
			key := relate.Key{Code: v.Code, Version: v.Version, Citation: n.Citation}
			title, present := titles[key]
			if !present && v.Access == "free" {
				title = n.PublisherTitle
			}
			if title != "" {
				row.Title = &title
			} else if v.Access == "licensed" {
				citation := n.Citation
				gaps = append(gaps, Gap{Kind: "title_missing", Code: v.Code, Version: v.Version, Citation: &citation, Count: 1})
			}
			rows = append(rows, row)
		}
		sort.Slice(rows, func(i, j int) bool { return rows[i].CitationNorm < rows[j].CitationNorm })
		controls = append(controls, rows...)
	}
	if err := writeRows(dir, "controls.jsonl", controls, frozen.stage); err != nil {
		return err
	}
	if err := writeRows(dir, "catalogs.jsonl", catalogs, frozen.stage); err != nil {
		return err
	}
	return writeGaps(dir, gaps, frozen.stage)
}

func validateLineage(nodes []parse.Node) error {
	ordinals := map[int]bool{}
	for i, n := range nodes {
		if n.Ordinal < 0 || ordinals[n.Ordinal] {
			return fmt.Errorf("invalid control ordinal")
		}
		ordinals[n.Ordinal] = true
		seen := map[int]bool{i: true}
		for parent := n.Parent; parent != -1; parent = nodes[parent].Parent {
			if parent < 0 || parent >= len(nodes) || seen[parent] {
				return fmt.Errorf("invalid control lineage")
			}
			seen[parent] = true
		}
	}
	return nil
}
