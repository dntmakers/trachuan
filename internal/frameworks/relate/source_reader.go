package relate

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"danny.vn/trachuan/internal/frameworks/parse"
	"danny.vn/trachuan/internal/frameworks/registry"
)

func readDirectSources(reg *registry.Registry, reader SourceReader) ([]DirectSource, error) {
	sources, err := registeredDirectSources(reg)
	if err != nil {
		return nil, err
	}
	var verified []DirectSource
	for _, source := range sources {
		v, err := sourceVersion(reg, source)
		if err != nil {
			return nil, err
		}
		if source.File.Role == "catalog" {
			selected, selectErr := v.SelectedSource()
			if selectErr != nil || selected != source.File {
				return nil, fmt.Errorf("source is not selected catalog")
			}
		}
		if !sourceApproved(v) {
			continue
		}
		if reader == nil {
			return nil, fmt.Errorf("missing source reader")
		}
		data, err := reader.ReadSource(v, source.File)
		if err != nil {
			return nil, fmt.Errorf("read registered source: %w", err)
		}
		digest := sha256.Sum256(data)
		if int64(len(data)) != source.File.Size || hex.EncodeToString(digest[:]) != source.File.SHA256 {
			return nil, fmt.Errorf("registered source digest or size mismatch")
		}
		source.Data = data
		if source.File.Role == "catalog" {
			source.Catalog, err = parse.Parse(v.Parser, data, v.Code, v.Version, v.ExpectedControls)
			if err != nil {
				return nil, fmt.Errorf("parse registered source: %w", err)
			}
			if source.Catalog == nil || len(source.Catalog.Nodes) == 0 {
				return nil, fmt.Errorf("empty registered source")
			}
		}
		verified = append(verified, source)
	}
	return verified, nil
}
