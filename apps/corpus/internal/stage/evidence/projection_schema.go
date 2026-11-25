package evidence

import (
	"encoding/json"
	"fmt"

	"danny.vn/trachuan/internal/releasechanges"
)

type schemaField struct {
	kind     string
	required bool
}
type objectSchema map[string]schemaField

func required(kind string) schemaField      { return schemaField{kind: kind, required: true} }
func optionalField(kind string) schemaField { return schemaField{kind: kind} }

func validateJSONSchema(raw []byte, schema objectSchema) error {
	if _, err := releasechanges.CanonicalPayload(raw); err != nil {
		return fmt.Errorf("invalid JSON object: %w", err)
	}
	var object projectionRecord
	if err := json.Unmarshal(raw, &object); err != nil {
		return fmt.Errorf("decode object: %w", err)
	}
	return validateObjectSchema(object, schema)
}

func validateObjectSchema(object projectionRecord, schema objectSchema) error {
	for name := range object {
		if _, ok := schema[name]; !ok {
			return fmt.Errorf("unknown field %s", name)
		}
	}
	for name, field := range schema {
		raw, ok := object[name]
		if !ok {
			if field.required {
				return fmt.Errorf("missing field %s", name)
			}
			continue
		}
		if err := validateSchemaValue(raw, field.kind); err != nil {
			return fmt.Errorf("field %s: %w", name, err)
		}
	}
	return nil
}

func validateSchemaValue(raw json.RawMessage, kind string) error {
	switch kind {
	case "string", "nullable_string":
		if kind == "nullable_string" && string(raw) == "null" {
			return nil
		}
		var value string
		if string(raw) == "null" || json.Unmarshal(raw, &value) != nil {
			return fmt.Errorf("expected string")
		}
		return nil
	case "bool":
		var value bool
		if string(raw) == "null" || json.Unmarshal(raw, &value) != nil {
			return fmt.Errorf("expected boolean")
		}
		return nil
	case "int", "positive":
		var value int64
		if string(raw) == "null" || json.Unmarshal(raw, &value) != nil || (kind == "positive" && value <= 0) {
			return fmt.Errorf("expected integer")
		}
		return nil
	case "strings", "validities":
		var values []json.RawMessage
		if string(raw) == "null" || json.Unmarshal(raw, &values) != nil {
			return fmt.Errorf("expected array")
		}
		element := "string"
		if kind == "validities" {
			element = "validity"
		}
		for _, v := range values {
			if err := validateSchemaValue(v, element); err != nil {
				return err
			}
		}
		return nil
	default:
		var nested projectionRecord
		if string(raw) == "null" || json.Unmarshal(raw, &nested) != nil || nested == nil {
			return fmt.Errorf("expected object")
		}
		schema := nestedSchema(kind)
		if schema == nil {
			return fmt.Errorf("unknown schema %s", kind)
		}
		return validateObjectSchema(nested, schema)
	}
}

func nestedSchema(kind string) objectSchema {
	switch kind {
	case "header":
		return objectSchema{"schema": required("int"), "bindings": required("bindings"), "baseline": required("bool")}
	case "bindings":
		return objectSchema{"rel": required("string"), "dump_sha256": required("string"), "build_artifact_sha256": required("string"), "report_sha256": required("string"), "predecessor_rel": required("string"), "predecessor_identity_sha256": required("string")}
	case "string_pair":
		return objectSchema{"before": required("string"), "after": required("string")}
	case "date_pair":
		return objectSchema{"before": required("nullable_string"), "after": required("nullable_string")}
	case "bool_pair":
		return objectSchema{"before": required("bool"), "after": required("bool")}
	case "validity":
		return objectSchema{"status_class": required("string"), "eff_from": required("nullable_string"), "eff_to": required("nullable_string"), "method": required("string")}
	case "end":
		return objectSchema{"key": required("nullable_string"), "number": required("string"), "in_release": required("bool"), "anchor": required("nullable_string"), "exact": optionalField("bool"), "named": optionalField("string"), "node": optionalField("string")}
	}
	return nil
}

func changeSchema(kind string) objectSchema {
	s := objectSchema{"type": required("string"), "key": required("string")}
	switch kind {
	case "document":
		s["change"] = required("string")
		s["cause"] = required("string")
		s["replaced_by"] = optionalField("string")
	case "document_key":
		s["before"] = required("string")
	case "document_legal":
		for _, n := range []string{"status_class"} {
			s[n] = optionalField("string_pair")
		}
		for _, n := range []string{"eff_from", "eff_to"} {
			s[n] = optionalField("date_pair")
		}
		for _, n := range []string{"is_current", "validity_conflict"} {
			s[n] = optionalField("bool_pair")
		}
	case "document_metadata":
		s["number_norm"] = required("string_pair")
	case "document_provenance":
		s["text_source"] = optionalField("string_pair")
		s["text_complete"] = optionalField("bool_pair")
		s["baseline"] = optionalField("bool_pair")
	case "provision", "provision_classification", "provision_validity":
		s["anchor"] = required("string")
		s["provenance"] = optionalField("bool")
		s["ambiguous"] = optionalField("bool")
		if kind == "provision" {
			s["change"] = required("string")
			s["text_sha256"] = required("date_pair")
		} else {
			element := "strings"
			if kind == "provision_validity" {
				element = "validities"
			}
			s["added"] = required(element)
			s["removed"] = required(element)
		}
	case "relation":
		return objectSchema{"type": required("string"), "kind": required("string"), "method": required("string"), "source": required("end"), "target": required("end"), "change": required("string"), "count": required("positive")}
	default:
		return nil
	}
	return s
}
