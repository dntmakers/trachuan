package fetch

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

func detectorObject(data []byte) (map[string]json.RawMessage, error) {
	return detectorObjectMode(data, false)
}

func detectorObjectMode(data []byte, allowNull bool) (map[string]json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return nil, fmt.Errorf("expected object")
	}
	fields := make(map[string]json.RawMessage)
	for decoder.More() {
		token, err = decoder.Token()
		if err != nil {
			return nil, fmt.Errorf("invalid field: %w", err)
		}
		key, ok := token.(string)
		if !ok {
			return nil, fmt.Errorf("invalid field name")
		}
		if _, exists := fields[key]; exists {
			return nil, fmt.Errorf("duplicate field %q", key)
		}
		var value json.RawMessage
		if err = decoder.Decode(&value); err != nil || (!allowNull && bytes.Equal(bytes.TrimSpace(value), []byte("null"))) {
			return nil, fmt.Errorf("invalid field %q", key)
		}
		fields[key] = value
	}
	if _, err = decoder.Token(); err != nil {
		return nil, fmt.Errorf("invalid object end: %w", err)
	}
	if _, err = decoder.Token(); err != io.EOF {
		return nil, fmt.Errorf("trailing object data")
	}
	return fields, nil
}

func detectorFields(fields map[string]json.RawMessage, required ...string) error {
	if len(fields) != len(required) {
		return fmt.Errorf("unexpected or missing fields")
	}
	for _, name := range required {
		if _, ok := fields[name]; !ok {
			return fmt.Errorf("missing field %q", name)
		}
	}
	return nil
}

func detectorState(fields map[string]json.RawMessage) (string, error) {
	var state string
	data, ok := fields["state"]
	if !ok || json.Unmarshal(data, &state) != nil || state == "" {
		return "", fmt.Errorf("invalid state")
	}
	return state, nil
}
