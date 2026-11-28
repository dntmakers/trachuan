package build

import (
	"context"
	"fmt"
	"reflect"
	"regexp"
	"sort"

	"danny.vn/trachuan/internal/frameworks/parse"
	"danny.vn/trachuan/internal/frameworks/registry"
)

type PDFRuntime interface {
	PDFExtractor
	RuntimeIdentity() string
	VerifyRuntime(context.Context) error
}

func availablePDFRuntime(runtime PDFRuntime) bool {
	if runtime == nil {
		return false
	}
	value := reflect.ValueOf(runtime)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return !value.IsNil()
	default:
		return true
	}
}

func pdfParseRecipe(reg *registry.Registry, identity string, tables ...parse.Table) (recipe, error) {
	var table parse.Table
	if len(tables) > 0 {
		table = tables[0]
	}
	r := stepRecipe("parse")
	needed := false
	selected := map[string]parse.PDFParser{}
	for _, version := range releaseVersions(reg) {
		entry, ok := table.Requirements(version.Parser)
		if !ok || entry.Revision == "" {
			return recipe{}, fmt.Errorf("invalid parser configuration")
		}
		selected[version.Parser] = entry
		needed = needed || table.NeedsPDF(version.Parser)
	}
	ids := make([]string, 0, len(selected))
	for id := range selected {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	parsers := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		entry := selected[id]
		parsers = append(parsers, map[string]any{"id": id, "revision": entry.Revision, "layout": entry.Layout, "bbox": entry.BBox})
	}
	r.Parameters["parsers"] = parsers
	if !needed {
		return r, nil
	}
	if identity == "" {
		return recipe{}, fmt.Errorf("PDF runtime identity required")
	}
	if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(identity) {
		return recipe{}, fmt.Errorf("invalid PDF runtime identity")
	}
	r.Parameters["pdf_runtime_sha256"] = identity
	return r, nil
}
