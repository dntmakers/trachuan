package evidence

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"

	"danny.vn/trachuan/internal/releasechanges"
)

func ProjectChanges(input io.ReadSeeker) ([]releasechanges.Row, error) {
	rows := []releasechanges.Row{}
	err := ProjectStream(input, func(row releasechanges.Row) error { rows = append(rows, row); return nil })
	return rows, err
}

func ProjectStream(input io.ReadSeeker, emit func(releasechanges.Row) error) error {
	pos, err := input.Seek(0, io.SeekCurrent)
	if err != nil {
		return fmt.Errorf("seek changes: %w", err)
	}
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 4096), 1024*1024+1)
	if !scanner.Scan() {
		return fmt.Errorf("line 1: missing header")
	}
	if err := validateJSONSchema(scanner.Bytes(), nestedSchema("header")); err != nil {
		return fmt.Errorf("line 1: %w", err)
	}
	var header struct {
		Schema   int             `json:"schema"`
		Baseline *bool           `json:"baseline"`
		Bindings changesBindings `json:"bindings"`
	}
	if err := json.Unmarshal(scanner.Bytes(), &header); err != nil {
		return fmt.Errorf("line 1: %w", err)
	}
	if header.Schema != 1 || header.Baseline == nil {
		return fmt.Errorf("line 1: invalid schema or baseline")
	}
	for _, value := range []string{header.Bindings.Rel, header.Bindings.DumpSHA256, header.Bindings.BuildArtifactSHA256, header.Bindings.ReportSHA256, header.Bindings.PredecessorRel, header.Bindings.PredecessorIdentitySHA256} {
		if strings.ContainsAny(value, "\t\n\x00") {
			return fmt.Errorf("line 1: invalid header field")
		}
	}
	initialHeader := bytes.Clone(scanner.Bytes())
	var firstHash []byte
	source := func(yield func(projectionRecord) error) error {
		if _, err := input.Seek(pos, io.SeekStart); err != nil {
			return fmt.Errorf("seek changes: %w", err)
		}
		hash := sha256.New()
		scanner := bufio.NewScanner(io.TeeReader(input, hash))
		scanner.Buffer(make([]byte, 4096), 1024*1024+1)
		if !scanner.Scan() {
			return fmt.Errorf("line 1: missing or oversized header")
		}
		if !bytes.Equal(scanner.Bytes(), initialHeader) {
			return fmt.Errorf("line 1: header changed")
		}
		line := 1
		for scanner.Scan() {
			line++
			if _, err := releasechanges.CanonicalPayload(scanner.Bytes()); err != nil {
				return fmt.Errorf("line %d: invalid JSON: %w", line, err)
			}
			var record projectionRecord
			if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
				return fmt.Errorf("line %d: %w", line, err)
			}
			if err := yield(record); err != nil {
				return fmt.Errorf("line %d: %w", line, err)
			}
		}
		if err := scanner.Err(); err != nil {
			return fmt.Errorf("line %d: %w", line+1, err)
		}
		sum := hash.Sum(nil)
		if firstHash == nil {
			firstHash = sum
		} else if !bytes.Equal(firstHash, sum) {
			return fmt.Errorf("changes stream changed between passes")
		}
		return nil
	}
	return projectValidated(*header.Baseline, source, emit)
}

func ProjectRecords(baseline bool, records func(func(any) error) error, emit func(releasechanges.Row) error) error {
	var firstHash []byte
	source := func(yield func(projectionRecord) error) error {
		ordinal := 0
		hash := sha256.New()
		err := records(func(value any) error {
			ordinal++
			b, err := encodeLine(value)
			if err != nil {
				return fmt.Errorf("record %d: %w", ordinal, err)
			}
			canonical, err := releasechanges.CanonicalPayload(b)
			if err != nil {
				return fmt.Errorf("record %d: invalid JSON: %w", ordinal, err)
			}
			_, _ = io.WriteString(hash, canonical)
			_, _ = io.WriteString(hash, "\n")
			var record projectionRecord
			if err := json.Unmarshal(b, &record); err != nil {
				return fmt.Errorf("record %d: %w", ordinal, err)
			}
			if err := yield(record); err != nil {
				return fmt.Errorf("record %d: %w", ordinal, err)
			}
			return nil
		})
		if err != nil {
			return err
		}
		sum := hash.Sum(nil)
		if firstHash == nil {
			firstHash = sum
		} else if !bytes.Equal(firstHash, sum) {
			return fmt.Errorf("generator records changed between passes")
		}
		return nil
	}
	return projectValidated(baseline, source, emit)
}

func projectValidated(baseline bool, source func(func(projectionRecord) error) error, emit func(releasechanges.Row) error) error {
	return projectValidatedLimit(baseline, source, emit, 128*1024*1024)
}

func projectValidatedLimit(baseline bool, source func(func(projectionRecord) error) error, emit func(releasechanges.Row) error, limit int) error {
	checked := func(yield func(projectionRecord) error) error {
		previousRank := 0
		previousID := ""
		return source(func(record projectionRecord) error {
			kind := projectionString(record, "type")
			ranks := map[string]int{"document": 1, "document_key": 2, "document_legal": 3, "document_metadata": 4, "document_provenance": 5, "provision": 6, "provision_classification": 7, "provision_validity": 8, "relation": 9}
			rank := ranks[kind]
			if rank == 0 {
				return fmt.Errorf("unknown record kind %q", kind)
			}
			if err := validateObjectSchema(record, changeSchema(kind)); err != nil {
				return err
			}
			if kind == "document" && projectionString(record, "change") == "added" {
				if _, ok := record["replaced_by"]; ok {
					return fmt.Errorf("added document has replacement")
				}
			}
			if kind == "relation" && projectionString(record, "change") != "added" && projectionString(record, "change") != "removed" {
				return fmt.Errorf("invalid relation change")
			}
			if err := validateProjectionRecord(record, kind); err != nil {
				return err
			}
			id := projectionString(record, "key") + "\x00" + projectionString(record, "anchor")
			if kind == "relation" {
				var r relationChange
				if err := json.Unmarshal(mustJSON(record), &r); err != nil {
					return err
				}
				b, err := encodeLine(relationIdentity{Kind: r.Kind, Method: r.Method, Source: r.Source, Target: r.Target})
				if err != nil {
					return err
				}
				id = string(b)
			}
			if rank < previousRank || (rank == previousRank && strings.Compare(id, previousID) <= 0) {
				return fmt.Errorf("duplicate or out-of-order %s identity", kind)
			}
			previousRank, previousID = rank, id
			return yield(record)
		})
	}
	var rows []releasechanges.Row
	total := 0
	err := projectRecords(baseline, checked, func(row releasechanges.Row) error {
		size := len(strconv.FormatInt(row.Ordinal, 10)) + len(row.Kind) + len(row.Payload) + 5
		if row.Key != nil {
			size += len(*row.Key)
		}
		if row.Key2 != nil {
			size += len(*row.Key2)
		}
		if size > limit-total {
			return fmt.Errorf("ordinal %d: projection exceeds %d bytes", row.Ordinal, limit)
		}
		total += size
		rows = append(rows, row)
		return nil
	})
	if err != nil {
		return err
	}
	for _, row := range rows {
		if err := emit(row); err != nil {
			return fmt.Errorf("ordinal %d: %w", row.Ordinal, err)
		}
	}
	return nil
}

func mustJSON(record projectionRecord) []byte { b, _ := json.Marshal(record); return b }

func validateProjectionRecord(r projectionRecord, kind string) error {
	required := []string{}
	switch kind {
	case "document":
		required = []string{"key", "change", "cause"}
	case "document_key":
		required = []string{"key", "before"}
	case "document_legal", "document_metadata", "document_provenance":
		required = []string{"key"}
	case "provision":
		required = []string{"key", "anchor", "change", "text_sha256"}
	case "provision_classification", "provision_validity":
		required = []string{"key", "anchor", "added", "removed"}
	case "relation":
		required = []string{"kind", "method", "source", "target", "change", "count"}
	}
	for _, name := range required {
		v, ok := r[name]
		if !ok || string(v) == "null" || string(v) == `""` {
			return fmt.Errorf("%s missing %s", kind, name)
		}
	}
	for _, name := range []string{"key", "before", "anchor", "cause", "change", "kind", "method", "replaced_by"} {
		if v, ok := r[name]; ok {
			var value string
			if err := json.Unmarshal(v, &value); err != nil || value == "" {
				return fmt.Errorf("invalid %s", name)
			}
			if (name == "key" || name == "before" || name == "replaced_by") && strings.ContainsAny(value, "\t\n\x00") {
				return fmt.Errorf("invalid %s", name)
			}
			if (name == "key" || name == "before" || name == "anchor" || name == "replaced_by") && utf8.RuneCountInString(value) > 512 {
				return fmt.Errorf("%s exceeds 512 code points", name)
			}
		}
	}
	if kind == "document" || kind == "provision" {
		change := projectionString(r, "change")
		if change != "added" && change != "removed" && (kind != "provision" || change != "changed") {
			return fmt.Errorf("invalid change %q", change)
		}
	}
	for _, name := range []string{"status_class", "eff_from", "eff_to", "is_current", "validity_conflict", "text_source", "text_complete", "baseline", "text_sha256", "number_norm"} {
		if v, ok := r[name]; ok {
			var pair map[string]json.RawMessage
			if err := json.Unmarshal(v, &pair); err != nil || pair["before"] == nil || pair["after"] == nil {
				return fmt.Errorf("invalid %s pair", name)
			}
			for _, side := range []string{"before", "after"} {
				value := pair[side]
				if (name == "eff_from" || name == "eff_to" || name == "text_sha256") && string(value) == "null" {
					continue
				}
				if name == "is_current" || name == "validity_conflict" || name == "text_complete" || name == "baseline" {
					var b bool
					if string(value) == "null" || json.Unmarshal(value, &b) != nil {
						return fmt.Errorf("invalid %s %s", name, side)
					}
				} else {
					var s string
					if string(value) == "null" || json.Unmarshal(value, &s) != nil {
						return fmt.Errorf("invalid %s %s", name, side)
					}
				}
			}
		}
	}
	if kind == "document_legal" || kind == "document_provenance" || kind == "document_metadata" {
		fields := map[string][]string{"document_legal": {"status_class", "eff_from", "eff_to", "is_current", "validity_conflict"}, "document_provenance": {"text_source", "text_complete", "baseline"}, "document_metadata": {"number_norm"}}
		found := false
		for _, field := range fields[kind] {
			if _, ok := r[field]; ok {
				found = true
			}
		}
		if !found {
			return fmt.Errorf("%s has no changed field", kind)
		}
	}
	if kind == "provision_validity" {
		for _, field := range []string{"added", "removed"} {
			var tuples []projectionRecord
			if err := json.Unmarshal(r[field], &tuples); err != nil || tuples == nil {
				return fmt.Errorf("invalid %s tuples", field)
			}
			for _, tuple := range tuples {
				for _, name := range []string{"status_class", "eff_from", "eff_to", "method"} {
					value, ok := tuple[name]
					if !ok {
						return fmt.Errorf("validity missing %s", name)
					}
					if (name == "eff_from" || name == "eff_to") && string(value) == "null" {
						continue
					}
					var s string
					if string(value) == "null" || json.Unmarshal(value, &s) != nil || s == "" {
						return fmt.Errorf("invalid validity %s", name)
					}
				}
			}
		}
	}
	for _, flag := range []string{"provenance", "ambiguous"} {
		if v, ok := r[flag]; ok {
			var b bool
			if err := json.Unmarshal(v, &b); err != nil || string(v) == "null" {
				return fmt.Errorf("invalid %s", flag)
			}
		}
	}
	return nil
}
