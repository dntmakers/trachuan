package build

import (
	"fmt"
	"reflect"
	"regexp"

	"danny.vn/trachuan/internal/frameworks/registry"
)

type PDFRuntime interface {
	RuntimeIdentity() string
	VerifyRuntime() error
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

func pdfParseRecipe(reg *registry.Registry, identity string) (recipe, error) {
	r := stepRecipe("parse")
	needed := false
	for _, version := range releaseVersions(reg) {
		var selected registry.File
		if version.ParseSource == "" {
			file, err := version.SelectedSource()
			if err != nil {
				return recipe{}, fmt.Errorf("select parse source: %w", err)
			}
			selected = file
		} else {
			found := false
			for _, file := range version.Files {
				if file.Path == version.ParseSource {
					selected, found = file, true
					break
				}
			}
			if !found {
				return recipe{}, fmt.Errorf("invalid parse source")
			}
		}
		needed = needed || selected.Format == "pdf"
	}
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
