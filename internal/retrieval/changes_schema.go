package retrieval

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"
	"unicode/utf8"

	"danny.vn/trachuan/internal/corpusdb"
	"danny.vn/trachuan/internal/releasechanges"
)

type ChangeSchema struct {
	Type                 string
	Format               string
	Nullable             bool
	MinLength            int
	MaxLength            int
	Enum                 []string
	AdditionalProperties bool
	Fields               map[string]ChangeField
}

type ChangeField struct {
	Required bool
	Schema   ChangeSchema
}

func ChangePayloadSchemas() map[string]ChangeSchema {
	str := ChangeSchema{Type: "string", MinLength: 1}
	key := ChangeSchema{Type: "string", Format: "document-key", MinLength: 1, MaxLength: ReadHandleMax}
	anchor := ChangeSchema{Type: "string", MinLength: 1, MaxLength: ReadHandleMax}
	date := ChangeSchema{Type: "string", Format: "date", Nullable: true}
	boolean := ChangeSchema{Type: "boolean"}
	enum := func(values ...string) ChangeSchema { return ChangeSchema{Type: "string", MinLength: 1, Enum: values} }
	status := enum("in_force", "partial", "expired", "not_yet", "suspended", "unknown")
	cause := enum("membership", "coverage", "source")
	textSource := enum("source_tree", "html_transcription", "pdf_text", "ocr", "none")
	required := func(s ChangeSchema) ChangeField { return ChangeField{Required: true, Schema: s} }
	optional := func(s ChangeSchema) ChangeField { return ChangeField{Schema: s} }
	object := func(fields map[string]ChangeField) ChangeSchema { return ChangeSchema{Type: "object", Fields: fields} }
	pair := func(s ChangeSchema) ChangeSchema {
		return object(map[string]ChangeField{"before": required(s), "after": required(s)})
	}
	source := object(map[string]ChangeField{"before_key": optional(key), "text_source": optional(pair(textSource)), "text_complete": optional(pair(boolean)), "baseline": optional(pair(boolean))})
	validity := object(map[string]ChangeField{"status_class": required(status), "eff_from": required(date), "eff_to": required(date), "method": required(str)})
	return map[string]ChangeSchema{
		"document_added":     object(map[string]ChangeField{"key": required(key), "cause": required(cause)}),
		"document_removed":   object(map[string]ChangeField{"key": required(key), "cause": required(cause), "replaced_by": optional(key)}),
		"document_key":       object(map[string]ChangeField{"key": required(key), "before_key": required(key)}),
		"document_validity":  object(map[string]ChangeField{"key": required(key), "source_change": optional(source), "status_class": optional(pair(status)), "eff_from": optional(pair(date)), "eff_to": optional(pair(date)), "is_current": optional(pair(boolean)), "validity_conflict": optional(pair(boolean))}),
		"provision_validity": object(map[string]ChangeField{"key": required(key), "anchor": required(anchor), "change": required(enum("added", "removed")), "validity": required(validity), "source_change": optional(source)}),
		"provision_text":     object(map[string]ChangeField{"key": required(key), "anchor": required(anchor), "change": required(enum("added", "removed", "changed")), "provenance": required(boolean), "ambiguous": required(boolean)}),
	}
}

func changeInvalid(reason string) error {
	return fmt.Errorf("invalid changes projection: %w", errors.Join(ErrUnavailable, &corpusdb.InvalidProjectionError{Err: errors.New(reason)}))
}

func validateChangePayload(row releasechanges.Row) (map[string]json.RawMessage, error) {
	schema, ok := ChangePayloadSchemas()[row.Kind]
	if !ok {
		return nil, changeInvalid("unknown kind")
	}
	canonical, err := releasechanges.CanonicalPayload([]byte(row.Payload))
	if err != nil || canonical != row.Payload {
		return nil, changeInvalid("noncanonical payload")
	}
	raw := json.RawMessage(row.Payload)
	if !validSchemaValue(raw, schema) {
		return nil, changeInvalid("invalid payload")
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return nil, changeInvalid("invalid payload")
	}
	var key string
	_ = json.Unmarshal(fields["key"], &key)
	if row.Key == nil || key != *row.Key {
		return nil, changeInvalid("key mismatch")
	}
	var secondary string
	switch row.Kind {
	case "document_key":
		_ = json.Unmarshal(fields["before_key"], &secondary)
	case "document_removed":
		_ = json.Unmarshal(fields["replaced_by"], &secondary)
	case "document_validity", "provision_validity":
		var source map[string]json.RawMessage
		_ = json.Unmarshal(fields["source_change"], &source)
		_ = json.Unmarshal(source["before_key"], &secondary)
	}
	if secondary == "" && row.Key2 != nil || secondary != "" && (row.Key2 == nil || *row.Key2 != secondary) {
		return nil, changeInvalid("secondary key mismatch")
	}
	return fields, nil
}

func validSchemaValue(raw json.RawMessage, schema ChangeSchema) bool {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return schema.Nullable
	}
	switch schema.Type {
	case "boolean":
		var value bool
		return json.Unmarshal(raw, &value) == nil
	case "string":
		var value string
		if json.Unmarshal(raw, &value) != nil || !utf8.ValidString(value) {
			return false
		}
		length := utf8.RuneCountInString(value)
		if length < schema.MinLength || schema.MaxLength > 0 && length > schema.MaxLength {
			return false
		}
		if len(schema.Enum) > 0 && !slices.Contains(schema.Enum, value) {
			return false
		}
		switch schema.Format {
		case "":
			return true
		case "document-key":
			return ValidDocumentKey(value)
		case "date":
			parsed, err := time.Parse(time.DateOnly, value)
			return err == nil && parsed.Year() >= 1 && parsed.Format(time.DateOnly) == value
		default:
			return false
		}
	case "object":
		var fields map[string]json.RawMessage
		if json.Unmarshal(raw, &fields) != nil || fields == nil {
			return false
		}
		for name, value := range fields {
			field, ok := schema.Fields[name]
			if !ok {
				if !schema.AdditionalProperties {
					return false
				}
				continue
			}
			if !validSchemaValue(value, field.Schema) {
				return false
			}
		}
		for name, field := range schema.Fields {
			if _, ok := fields[name]; field.Required && !ok {
				return false
			}
		}
		return true
	}
	return false
}
