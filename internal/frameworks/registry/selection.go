package registry

import "fmt"

func (f Framework) SelectedSource() (File, error) {
	if f.ParseSource == "" {
		if f.Parser == "csa-ccm-4-1-xlsx" {
			return File{}, fmt.Errorf("parse source required")
		}
		var found File
		count := 0
		for _, file := range f.Files {
			if file.Role == "catalog" {
				found = file
				count++
			}
		}
		if count != 1 {
			return File{}, fmt.Errorf("expected one catalog")
		}
		return found, nil
	}
	for _, file := range f.Files {
		if file.Path != f.ParseSource {
			continue
		}
		switch f.Parser {
		case "csa-ccm-4-1-xlsx":
			if file.Role == "catalog" && file.Format == "xlsx" {
				return file, nil
			}
		}
		return File{}, fmt.Errorf("invalid parse source type")
	}
	return File{}, fmt.Errorf("parse source not registered")
}
