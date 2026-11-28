package registry

import (
	"fmt"

	"danny.vn/trachuan/internal/frameworks/parse"
)

func (f Framework) SelectedSource() (File, error) { return f.SelectedSourceWithTable(nil) }

func (f Framework) SelectedSourceWithTable(table parse.Table) (File, error) {
	entry, required := table.Requirements(f.Parser)
	role, format := entry.Role, entry.Format
	if !required {
		return File{}, fmt.Errorf("unknown parser")
	}
	if f.ParseSource == "" {
		if !entry.Legacy {
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
	var found File
	count := 0
	for _, file := range f.Files {
		if file.Path == f.ParseSource {
			found = file
			count++
		}
	}
	if count != 1 {
		return File{}, fmt.Errorf("parse source not registered uniquely")
	}
	if entry.Legacy || found.Role != role || found.Format != format {
		return File{}, fmt.Errorf("invalid parse source type")
	}
	return found, nil
}
