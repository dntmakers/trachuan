package classify

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	"danny.vn/trachuan/apps/corpus/internal/batchapi"
)

func validateRetainedJSON(raw []byte, value any) error {
	typ := reflect.TypeOf(value)
	if typ == nil {
		return fmt.Errorf("missing retained record type")
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return fmt.Errorf("null retained record")
	}
	return validateRetainedValue(raw, typ)
}

func validateNullableRetainedCollection(raw []byte, value any) error {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		typ := reflect.TypeOf(value)
		if typ != nil && typ.Kind() == reflect.Pointer && (typ.Elem().Kind() == reflect.Slice || typ.Elem().Kind() == reflect.Map) {
			return nil
		}
		return fmt.Errorf("null retained record")
	}
	return validateRetainedJSON(raw, value)
}

func validateRetainedValue(raw []byte, typ reflect.Type) error {
	raw = bytes.TrimSpace(raw)
	if typ == nil {
		return nil
	}
	if bytes.Equal(raw, []byte("null")) {
		if typ == reflect.TypeOf(json.RawMessage{}) || typ.Kind() == reflect.Pointer || typ.Kind() == reflect.Map || typ.Kind() == reflect.Slice || typ.Kind() == reflect.Interface {
			return nil
		}
		return fmt.Errorf("null retained scalar")
	}
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ == reflect.TypeOf(json.RawMessage{}) {
		return nil
	}
	if typ == reflect.TypeOf(time.Time{}) {
		var timestamp time.Time
		return json.Unmarshal(raw, &timestamp)
	}
	switch typ.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if len(raw) > 0 && raw[0] == '-' {
			n, err := strconv.ParseInt(string(raw), 10, 64)
			if err != nil || strconv.FormatInt(n, 10) != string(raw) {
				return fmt.Errorf("invalid retained integer")
			}
		} else if _, ok := batchapi.TokenInteger(raw, 0); !ok {
			return fmt.Errorf("invalid retained integer")
		}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if _, ok := batchapi.TokenInteger(raw, 0); !ok {
			return fmt.Errorf("invalid retained unsigned integer")
		}
	case reflect.Float32, reflect.Float64:
		var n float64
		if err := json.Unmarshal(raw, &n); err != nil {
			return err
		}
	case reflect.String:
		var text string
		if len(raw) == 0 || raw[0] != '"' || json.Unmarshal(raw, &text) != nil {
			return fmt.Errorf("invalid retained string")
		}
	case reflect.Bool:
		if string(raw) != "true" && string(raw) != "false" {
			return fmt.Errorf("invalid retained boolean")
		}

	case reflect.Struct:
		fields := map[string]reflect.Type{}
		retainedFields(typ, fields)
		names := make([]string, 0, len(fields))
		for name := range fields {
			names = append(names, name)
		}
		object, err := batchapi.DecodeEvidenceObject(raw, names...)
		if err != nil {
			return err
		}
		required := retainedRequired(typ)
		conditional, err := retainedConditionalRequired(typ, object)
		if err != nil {
			return err
		}
		required = append(required, conditional...)
		for _, name := range required {
			value, found := object[name]
			if !found || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
				return fmt.Errorf("missing or null required retained field %s", name)
			}
		}
		for name, value := range object {
			if nested, ok := fields[name]; ok {
				if nested.Kind() == reflect.Pointer && bytes.Equal(bytes.TrimSpace(value), []byte("null")) && !retainedNullable(typ, name) {
					return fmt.Errorf("null retained pointer %s", name)
				}
				if err = validateRetainedValue(value, nested); err != nil {
					return fmt.Errorf("invalid retained %s: %w", name, err)
				}
			}
		}
	case reflect.Map:
		object, err := batchapi.DecodeObject(raw)
		if err != nil {
			return err
		}
		for _, value := range object {
			if err = validateRetainedValue(value, typ.Elem()); err != nil {
				return err
			}
		}
	case reflect.Slice, reflect.Array:
		if typ.Elem().Kind() == reflect.Uint8 {
			var data []byte
			return json.Unmarshal(raw, &data)
		}
		var values []json.RawMessage
		if err := json.Unmarshal(raw, &values); err != nil {
			return err
		}
		for _, value := range values {
			if err := validateRetainedValue(value, typ.Elem()); err != nil {
				return err
			}
		}
	}
	return nil
}

func retainedFields(typ reflect.Type, fields map[string]reflect.Type) {
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if field.Anonymous && field.Type.Kind() == reflect.Struct {
			retainedFields(field.Type, fields)
			continue
		}
		if field.PkgPath != "" {
			continue
		}
		name := strings.Split(field.Tag.Get("json"), ",")[0]
		if name == "-" {
			continue
		}
		if name == "" {
			name = field.Name
		}
		fields[name] = field.Type
	}
}

func retainedNullable(typ reflect.Type, name string) bool {
	return (typ == reflect.TypeOf(Submission{}) && (name == "submitted_at" || name == "deadline_at")) || (typ == reflect.TypeOf(attemptRecord{}) && name == "cost_micro_usd")
}
