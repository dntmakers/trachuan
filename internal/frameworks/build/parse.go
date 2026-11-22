package build

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"

	"danny.vn/trachuan/internal/frameworks/parse"
	"danny.vn/trachuan/internal/frameworks/registry"
	"danny.vn/trachuan/internal/frameworks/relate"
)

type catalog struct {
	Code    string       `json:"code"`
	Version string       `json:"version"`
	Nodes   []parse.Node `json:"nodes"`
}

func parseStep(ctx context.Context, base, dir string) error {
	snapshot := filepath.Join(base, "snapshot")
	reg, err := readRegistry(filepath.Join(snapshot, "sources/frameworks.yaml"))
	if err != nil {
		return err
	}
	titles, err := readTitles(filepath.Join(snapshot, "trachuan/titles.csv"))
	if err != nil {
		return err
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
		result, parseErr := parseVersion(snapshot, v)
		if parseErr != nil {
			return parseErr
		}
		if lineageErr := validateLineage(result.Nodes); lineageErr != nil {
			return lineageErr
		}
		catalogs = append(catalogs, catalog{v.Code, v.Version, result.Nodes})
		titleCatalog.add(v, result.Nodes)
	}
	titles, err = titleCatalog.validate(titles, false)
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
	if err := writeRows(dir, "controls.jsonl", controls); err != nil {
		return err
	}
	if err := writeRows(dir, "catalogs.jsonl", catalogs); err != nil {
		return err
	}
	return writeGaps(dir, gaps)
}

func parseVersion(snapshot string, v registry.Framework) (*parse.Result, error) {
	file, err := v.SelectedSource()
	if err != nil {
		return nil, err
	}
	data, err := readFile(filepath.Join(snapshot, "files", file.Path))
	if err != nil {
		return nil, err
	}
	result, err := parse.Parse(v.Parser, data, v.Code, v.Version, v.ExpectedControls)
	if err != nil {
		return nil, hide("parse catalog", err)
	}
	return result, nil
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
