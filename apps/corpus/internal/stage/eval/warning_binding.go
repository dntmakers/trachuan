package eval

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"danny.vn/trachuan/apps/corpus/internal/evaluation"
)

func bindBuildWarnings(dir, country string, cases []evaluation.WarningCase) ([]evaluation.WarningResult, error) {
	read := func(name string) ([]byte, error) {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", name, err)
		}
		return data, nil
	}
	knowledge, err := read("knowledge.yaml")
	if err != nil {
		return nil, err
	}
	report, err := read("knowledge-report.json")
	if err != nil {
		return nil, err
	}
	return evaluation.BindWarningCases(cases, country, knowledge, report)
}
