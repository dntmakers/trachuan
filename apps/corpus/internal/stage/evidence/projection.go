package evidence

import (
	"bytes"
	"encoding/json"
	"fmt"
	"unicode/utf8"

	"danny.vn/trachuan/internal/releasechanges"
)

type projectionRecord map[string]json.RawMessage

func projectionString(r projectionRecord, key string) string {
	var s string
	_ = json.Unmarshal(r[key], &s)
	return s
}

func projectionBool(r projectionRecord, key string) bool {
	var b bool
	_ = json.Unmarshal(r[key], &b)
	return b
}

func projectionFields(r projectionRecord, names ...string) projectionRecord {
	out := projectionRecord{}
	for _, name := range names {
		if v, ok := r[name]; ok {
			out[name] = v
		}
	}
	return out
}

func compactContextValue(raw json.RawMessage) (json.RawMessage, error) {
	if raw == nil {
		return nil, nil
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, raw); err != nil {
		return nil, fmt.Errorf("compact source context: %w", err)
	}
	return bytes.Clone(compact.Bytes()), nil
}

type sourceContext struct {
	beforeKey    json.RawMessage
	textSource   json.RawMessage
	textComplete json.RawMessage
	baseline     json.RawMessage
}

func (s sourceContext) fields() projectionRecord {
	out := projectionRecord{}
	if s.beforeKey != nil {
		out["before_key"] = s.beforeKey
	}
	if s.textSource != nil {
		out["text_source"] = s.textSource
	}
	if s.textComplete != nil {
		out["text_complete"] = s.textComplete
	}
	if s.baseline != nil {
		out["baseline"] = s.baseline
	}
	return out
}

func projectRecords(baseline bool, source func(func(projectionRecord) error) error, emit func(releasechanges.Row) error) error {
	context := map[string]sourceContext{}
	if err := source(func(record projectionRecord) error {
		kind, key := projectionString(record, "type"), projectionString(record, "key")
		if baseline {
			return fmt.Errorf("baseline has change records")
		}
		if kind == "document_key" || kind == "document_provenance" {
			state, exists := context[key]
			if !exists && len(context) >= 10000 {
				return fmt.Errorf("source context exceeds 10000 documents")
			}
			for _, pair := range []struct {
				name   string
				target *json.RawMessage
			}{{"before", &state.beforeKey}, {"text_source", &state.textSource}, {"text_complete", &state.textComplete}, {"baseline", &state.baseline}} {
				if raw, ok := record[pair.name]; ok {
					value, err := compactContextValue(raw)
					if err != nil {
						return err
					}
					*pair.target = value
				}
			}
			if len(state.beforeKey)+len(state.textSource)+len(state.textComplete)+len(state.baseline) > 4096 {
				return fmt.Errorf("source context exceeds 4096 bytes")
			}
			encoded, err := json.Marshal(state.fields())
			if err != nil {
				return fmt.Errorf("encode source context: %w", err)
			}
			if len(encoded) > 4096 {
				return fmt.Errorf("source context exceeds 4096 bytes")
			}
			context[key] = state
		}
		return nil
	}); err != nil {
		return err
	}
	var ordinal int64
	appendRow := func(kind, key, key2 string, payload projectionRecord) error {
		b, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("encode payload: %w", err)
		}
		item := projectionFields(payload)
		for k, v := range payload {
			item[k] = v
		}
		item["kind"], _ = json.Marshal(kind)
		if kind != "document_removed" && (kind != "provision_text" || (projectionString(payload, "change") != "removed" && !projectionBool(payload, "ambiguous"))) {
			handle := key
			if kind == "provision_text" || kind == "provision_validity" {
				handle += "#" + projectionString(payload, "anchor")
			}
			if utf8.RuneCountInString(handle) <= 512 {
				item["handle"], _ = json.Marshal(handle)
			}
		}
		encodedItem, err := json.Marshal(item)
		if err != nil {
			return fmt.Errorf("encode item: %w", err)
		}
		if len(encodedItem) > 4096 {
			return fmt.Errorf("ordinal %d: projection item exceeds 4096 bytes", ordinal+1)
		}
		canonical, err := releasechanges.CanonicalPayload(b)
		if err != nil {
			return fmt.Errorf("canonical payload: %w", err)
		}
		var second *string
		if key2 != "" {
			second = &key2
		}
		ordinal++
		return emit(releasechanges.Row{Ordinal: ordinal, Kind: kind, Key: &key, Key2: second, Payload: canonical})
	}
	return source(func(record projectionRecord) error {
		kind, key := projectionString(record, "type"), projectionString(record, "key")
		var payload projectionRecord
		key2 := ""
		switch kind {
		case "document":
			change := projectionString(record, "change")
			if change != "added" && change != "removed" {
				return fmt.Errorf("invalid document change %q", change)
			}
			kind = "document_" + change
			payload = projectionFields(record, "key", "cause", "replaced_by")
			key2 = projectionString(record, "replaced_by")
		case "document_key":
			payload = projectionFields(record, "key")
			payload["before_key"] = record["before"]
			key2 = projectionString(record, "before")
		case "document_legal":
			kind = "document_validity"
			payload = projectionFields(record, "key", "status_class", "eff_from", "eff_to", "is_current", "validity_conflict")
			if state, ok := context[key]; ok {
				source := state.fields()
				b, err := json.Marshal(source)
				if err != nil {
					return fmt.Errorf("encode source change: %w", err)
				}
				payload["source_change"] = b
				key2 = projectionString(source, "before_key")
			}
		case "provision":
			kind = "provision_text"
			payload = projectionFields(record, "key", "anchor", "change")
			for _, flag := range []string{"provenance", "ambiguous"} {
				b, _ := json.Marshal(projectionBool(record, flag))
				payload[flag] = b
			}
		case "provision_validity":
			if projectionBool(record, "provenance") || projectionBool(record, "ambiguous") {
				return nil
			}
			for _, change := range []string{"added", "removed"} {
				var tuples []json.RawMessage
				if err := json.Unmarshal(record[change], &tuples); err != nil {
					return fmt.Errorf("decode validity %s: %w", change, err)
				}
				for _, tuple := range tuples {
					p := projectionFields(record, "key", "anchor")
					p["change"], _ = json.Marshal(change)
					p["validity"] = tuple
					if state, ok := context[key]; ok {
						source := state.fields()
						b, err := json.Marshal(source)
						if err != nil {
							return fmt.Errorf("encode source change: %w", err)
						}
						p["source_change"] = b
						key2 = projectionString(source, "before_key")
					}
					if err := appendRow(kind, key, key2, p); err != nil {
						return err
					}
				}
			}
			return nil
		default:
			return nil
		}
		return appendRow(kind, key, key2, payload)
	})
}
