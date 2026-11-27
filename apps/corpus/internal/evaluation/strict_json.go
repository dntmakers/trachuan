package evaluation

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const maxReportDepth = 1000

func DecodeReport(data []byte) (Report, error) {
	var report Report
	if err := UnmarshalStrict(data, &report); err != nil {
		return Report{}, err
	}
	return report, nil
}

func UnmarshalStrict(data []byte, value any) error {
	if err := rejectDuplicateKeys(data); err != nil {
		return err
	}
	if err := json.Unmarshal(data, value); err != nil {
		return fmt.Errorf("decode report: %w", err)
	}
	return nil
}

func rejectDuplicateKeys(data []byte) error {
	if !utf8.Valid(data) {
		return fmt.Errorf("decode report: invalid UTF-8")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	return scanValue(d, 0)
}

func foldKey(key string) string {
	var out strings.Builder
	for _, r := range key {
		least := r
		for next := unicode.SimpleFold(r); next != r; next = unicode.SimpleFold(next) {
			least = min(least, next)
		}
		out.WriteRune(least)
	}
	return out.String()
}

func scanValue(d *json.Decoder, depth int) error {
	if depth > maxReportDepth {
		return fmt.Errorf("decode report: nesting deeper than %d", maxReportDepth)
	}
	token, err := d.Token()
	if err != nil {
		return fmt.Errorf("decode report: %w", err)
	}
	delim, ok := token.(json.Delim)
	if !ok {
		if number, isNumber := token.(json.Number); isNumber && strings.ContainsAny(string(number), ".eE") {
			if f, parseErr := strconv.ParseFloat(string(number), 64); parseErr != nil || math.IsInf(f, 0) {
				return fmt.Errorf("decode report: non-finite number %s", number)
			}
		}
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]string{}
		for d.More() {
			keyToken, keyErr := d.Token()
			if keyErr != nil {
				return fmt.Errorf("decode report: %w", keyErr)
			}
			key, isString := keyToken.(string)
			if !isString {
				return fmt.Errorf("decode report: invalid object key")
			}
			if other, dup := seen[foldKey(key)]; dup {
				return fmt.Errorf("decode report: duplicate JSON field %q (also %q)", key, other)
			}
			seen[foldKey(key)] = key
			if err = scanValue(d, depth+1); err != nil {
				return err
			}
		}
	case '[':
		for d.More() {
			if err = scanValue(d, depth+1); err != nil {
				return err
			}
		}
	}
	if _, err = d.Token(); err != nil {
		return fmt.Errorf("decode report: %w", err)
	}
	return nil
}
