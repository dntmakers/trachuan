package batchapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
)

func DecodeObject(b []byte) (map[string]json.RawMessage, error) {
	return decodeEvidenceObject(b, false, nil)
}

func DecodeEvidenceObject(b []byte, names ...string) (map[string]json.RawMessage, error) {
	return decodeEvidenceObject(b, false, names)
}

func DecodeEvidenceProjection(b []byte, names ...string) (map[string]json.RawMessage, error) {
	return decodeEvidenceObject(b, true, names)
}

func decodeEvidenceObject(b []byte, project bool, names []string) (map[string]json.RawMessage, error) {
	dec := json.NewDecoder(bytes.NewReader(b))
	token, err := dec.Token()
	if err != nil || token != json.Delim('{') {
		return nil, fmt.Errorf("expected JSON object")
	}
	out := map[string]json.RawMessage{}
	for dec.More() {
		token, err = dec.Token()
		if err != nil {
			return nil, fmt.Errorf("read JSON key: %w", err)
		}
		key, ok := token.(string)
		if !ok {
			return nil, fmt.Errorf("invalid JSON key")
		}
		selected := false
		for _, name := range names {
			if strings.EqualFold(key, name) {
				if key != name {
					return nil, fmt.Errorf("case-variant evidence field")
				}
				selected = true
				break
			}
		}
		var raw json.RawMessage
		if err = dec.Decode(&raw); err != nil {
			return nil, fmt.Errorf("decode JSON value: %w", err)
		}
		if project && !selected {
			continue
		}
		if _, exists := out[key]; exists {
			return nil, fmt.Errorf("invalid or duplicate JSON key")
		}
		out[key] = raw
	}
	end, err := dec.Token()
	if err != nil {
		return nil, fmt.Errorf("close JSON object: %w", err)
	}
	if end != json.Delim('}') {
		return nil, fmt.Errorf("unexpected JSON object terminator")
	}
	var trailing any
	if dec.Decode(&trailing) != io.EOF {
		return nil, fmt.Errorf("unexpected trailing JSON data")
	}
	return out, nil
}

func TokenInteger(raw json.RawMessage, minValue int64) (int64, bool) {
	if len(raw) == 0 || (len(raw) > 1 && raw[0] == '0') {
		return 0, false
	}
	for _, digit := range raw {
		if digit < '0' || digit > '9' {
			return 0, false
		}
	}
	n, err := strconv.ParseInt(string(raw), 10, 64)
	return n, err == nil && n >= minValue
}
