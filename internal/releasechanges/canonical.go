package releasechanges

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

func validateUnicodeEscapes(input []byte) error {
	quoted := false
	for i := 0; i < len(input); i++ {
		if !quoted {
			if input[i] == '"' {
				quoted = true
			}
			continue
		}
		if input[i] == '"' {
			quoted = false
			continue
		}
		if input[i] != '\\' {
			continue
		}
		i++
		if i >= len(input) {
			return fmt.Errorf("incomplete escape")
		}
		if input[i] != 'u' {
			continue
		}
		if i+4 >= len(input) {
			return fmt.Errorf("incomplete Unicode escape")
		}
		value, err := strconv.ParseUint(string(input[i+1:i+5]), 16, 16)
		if err != nil {
			return fmt.Errorf("invalid Unicode escape: %w", err)
		}
		i += 4
		if value >= 0xdc00 && value <= 0xdfff {
			return fmt.Errorf("unpaired low surrogate")
		}
		if value >= 0xd800 && value <= 0xdbff {
			if i+6 >= len(input) || input[i+1] != '\\' || input[i+2] != 'u' {
				return fmt.Errorf("unpaired high surrogate")
			}
			low, err := strconv.ParseUint(string(input[i+3:i+7]), 16, 16)
			if err != nil || low < 0xdc00 || low > 0xdfff {
				return fmt.Errorf("unpaired high surrogate")
			}
			i += 6
		}
	}
	return nil
}

func parsePayload(input []byte) (map[string]any, error) {
	if !utf8.Valid(input) {
		return nil, fmt.Errorf("invalid UTF-8")
	}
	if err := validateUnicodeEscapes(input); err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(input))
	decoder.UseNumber()
	value, err := decodeValue(decoder)
	if err != nil {
		return nil, err
	}
	object, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("payload is not an object")
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, fmt.Errorf("trailing JSON")
	}
	return object, nil
}

func CanonicalPayload(input []byte) (string, error) {
	value, err := parsePayload(input)
	if err != nil {
		return "", err
	}
	var out strings.Builder
	if err := canonicalValue(&out, value); err != nil {
		return "", err
	}
	return out.String(), nil
}

func decodeValue(decoder *json.Decoder) (any, error) {
	token, err := decoder.Token()
	if err != nil {
		return nil, fmt.Errorf("decode payload: %w", err)
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		if number, ok := token.(json.Number); ok && strings.ContainsAny(string(number), ".eE") {
			return nil, fmt.Errorf("non-integer number")
		}
		return token, nil
	}
	switch delimiter {
	case '{':
		object := map[string]any{}
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return nil, err
			}
			key, ok := keyToken.(string)
			if !ok {
				return nil, fmt.Errorf("invalid object key")
			}
			if _, exists := object[key]; exists {
				return nil, fmt.Errorf("duplicate key")
			}
			value, err := decodeValue(decoder)
			if err != nil {
				return nil, err
			}
			object[key] = value
		}
		if _, err := decoder.Token(); err != nil {
			return nil, err
		}
		return object, nil
	case '[':
		var values []any
		for decoder.More() {
			value, err := decodeValue(decoder)
			if err != nil {
				return nil, err
			}
			values = append(values, value)
		}
		if _, err := decoder.Token(); err != nil {
			return nil, err
		}
		return values, nil
	default:
		return nil, fmt.Errorf("invalid JSON delimiter")
	}
}

func canonicalString(out *strings.Builder, value string) {
	out.WriteByte('"')
	for _, r := range value {
		switch {
		case r == '"':
			out.WriteString(`\"`)
		case r == '\\':
			out.WriteString(`\\`)
		case r < 0x20:
			out.WriteString(`\u00`)
			out.WriteByte("0123456789abcdef"[byte(r)>>4])
			out.WriteByte("0123456789abcdef"[byte(r)&15])
		default:
			out.WriteRune(r)
		}
	}
	out.WriteByte('"')
}

func canonicalValue(out *strings.Builder, value any) error {
	switch v := value.(type) {
	case nil:
		out.WriteString("null")
	case bool:
		out.WriteString(strconv.FormatBool(v))
	case string:
		canonicalString(out, v)
	case json.Number:
		if strings.ContainsAny(string(v), ".eE") {
			return fmt.Errorf("non-integer number")
		}
		number, ok := new(big.Int).SetString(string(v), 10)
		if !ok {
			return fmt.Errorf("invalid integer")
		}
		out.WriteString(number.String())
	case []any:
		out.WriteByte('[')
		for i, element := range v {
			if i > 0 {
				out.WriteByte(',')
			}
			if err := canonicalValue(out, element); err != nil {
				return err
			}
		}
		out.WriteByte(']')
	case map[string]any:
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		out.WriteByte('{')
		for i, key := range keys {
			if i > 0 {
				out.WriteByte(',')
			}
			canonicalString(out, key)
			out.WriteByte(':')
			if err := canonicalValue(out, v[key]); err != nil {
				return err
			}
		}
		out.WriteByte('}')
	default:
		return fmt.Errorf("invalid JSON value")
	}
	return nil
}
