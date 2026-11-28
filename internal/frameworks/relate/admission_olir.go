package relate

import (
	"bytes"
	"fmt"
	"strings"

	"danny.vn/trachuan/internal/frameworks/parse"
	"github.com/xuri/excelize/v2"
)

func admissionOLIR(data []byte) (*parse.Result, []string, int, error) {
	f, err := excelize.OpenReader(bytes.NewReader(data), excelize.Options{UnzipSizeLimit: 256 << 20, UnzipXMLSizeLimit: 256 << 20})
	if err != nil {
		return nil, nil, 0, fmt.Errorf("invalid OLIR workbook: %w", err)
	}
	defer func() { _ = f.Close() }()
	result := &parse.Result{}
	found := false
	var evidence []string
	dropped := 0
	for _, sheet := range f.GetSheetList() {
		if sheet == "Definitions" {
			continue
		}
		rows, err := f.GetRows(sheet)
		if err != nil {
			return nil, nil, 0, fmt.Errorf("invalid OLIR sheet: %w", err)
		}
		if len(rows) == 0 {
			continue
		}
		headers := map[string]int{}
		for i, h := range rows[0] {
			headers[strings.ToLower(strings.Join(strings.Fields(h), " "))] = i
		}
		focal, ok := headers["focal document element"]
		if !ok {
			continue
		}
		target, ok := headers["reference document element"]
		if !ok {
			continue
		}
		found = true
		comments, hasComments := headers["comments (optional)"]
		for _, row := range rows[1:] {
			rawFrom := cellValue(row, focal)
			from := nistCitation(rawFrom)
			to := cellValue(row, target)
			if from == "" || !isoCitation(to) {
				if rawFrom != "" || to != "" {
					dropped++
				}
				continue
			}
			payload := ""
			if hasComments {
				payload = cellValue(row, comments)
			}
			evidence = append(evidence, payload)
			result.Nodes = append(result.Nodes, parse.Node{Citation: from, Mappings: []parse.Mapping{{TargetFramework: "iso27001", TargetVersion: "2022", TargetCitation: to, Relation: "related"}}})
		}
	}
	if !found {
		return nil, nil, 0, fmt.Errorf("missing OLIR sheet")
	}
	return result, evidence, dropped, nil
}
