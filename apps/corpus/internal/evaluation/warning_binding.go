package evaluation

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"slices"

	"danny.vn/trachuan/internal/knowledge"
	"danny.vn/trachuan/internal/retrieval"
)

func BindWarningCases(cases []WarningCase, country string, source, report []byte) ([]WarningResult, error) {
	if (source == nil) != (report == nil) {
		return nil, fmt.Errorf("knowledge file and report must appear together")
	}
	file := knowledge.File{}
	var rows map[string]knowledge.Record
	var err error
	if source != nil {
		file, err = knowledge.Parse(country, source)
		if err != nil {
			return nil, fmt.Errorf("parse knowledge: %w", err)
		}
		rows, err = decodeKnowledgeOutcomes(report)
		if err != nil {
			return nil, err
		}
	}
	claims := map[string]knowledge.Claim{}
	decisions := map[string]bool{}
	for _, claim := range file.Claims {
		claims[claim.ID] = claim
	}
	for _, decision := range file.Decisions {
		decisions[decision.ID] = true
	}
	for id, row := range rows {
		claim, ok := claims[id]
		if !ok || row.Kind != claim.Kind || row.TargetKey != claim.TargetKey {
			return nil, fmt.Errorf("invalid knowledge outcome binding %q", id)
		}
	}
	for id := range claims {
		if _, ok := rows[id]; !ok {
			return nil, fmt.Errorf("missing knowledge outcome %q", id)
		}
	}
	results := make([]WarningResult, 0, len(cases))
	for _, c := range cases {
		result := WarningResult{EntryID: c.EntryID, Outcome: "missing", Search: []WarningSearch{}}
		claim, ok := claims[c.EntryID]
		if decisions[c.EntryID] {
			return nil, fmt.Errorf("warning entry %q has wrong kind", c.EntryID)
		}
		if ok {
			if claim.Kind != knowledge.KindEffect || retrieval.Handle(retrieval.Identity{DocumentKey: claim.TargetKey, Anchor: claim.TargetAnchor}) != c.Target || retrieval.Handle(retrieval.Identity{DocumentKey: claim.ActingKey, Anchor: claim.ActingAnchor}) != c.Acting || claim.RelationKind != c.RelationKind {
				return nil, fmt.Errorf("warning entry %q contradicts fixture", c.EntryID)
			}
			result.Outcome, result.Reason = rows[c.EntryID].Outcome, rows[c.EntryID].Reason
		}
		results = append(results, result)
	}
	return results, nil
}

func decodeKnowledgeOutcomes(data []byte) (map[string]knowledge.Record, error) {
	d := json.NewDecoder(bytes.NewReader(data))
	tok, err := d.Token()
	if err != nil || tok != json.Delim('[') {
		return nil, fmt.Errorf("invalid knowledge outcomes array")
	}
	rows := map[string]knowledge.Record{}
	for d.More() {
		tok, err = d.Token()
		if err != nil || tok != json.Delim('{') {
			return nil, fmt.Errorf("invalid knowledge outcome")
		}
		fields := map[string]string{}
		for d.More() {
			tok, err = d.Token()
			key, ok := tok.(string)
			if err != nil || !ok || !slices.Contains([]string{"id", "kind", "target_key", "outcome", "reason"}, key) {
				return nil, fmt.Errorf("invalid knowledge outcome field")
			}
			if _, ok = fields[key]; ok {
				return nil, fmt.Errorf("duplicate knowledge outcome field")
			}
			var value any
			if err = d.Decode(&value); err != nil {
				return nil, fmt.Errorf("decode knowledge outcome: %w", err)
			}
			text, ok := value.(string)
			if !ok {
				return nil, fmt.Errorf("invalid knowledge outcome value")
			}
			fields[key] = text
		}
		if tok, err = d.Token(); err != nil || tok != json.Delim('}') {
			return nil, fmt.Errorf("invalid knowledge outcome object")
		}
		r := knowledge.Record{ID: fields["id"], Kind: fields["kind"], TargetKey: fields["target_key"], Outcome: fields["outcome"], Reason: fields["reason"]}
		_, reasonPresent := fields["reason"]
		validReason := slices.Contains([]string{"document_absent", "baseline", "incomplete", "anchor_absent", "ambiguous", "contract", "number_norm", "text_source", "text", "heading", "ancestors"}, r.Reason)
		if r.ID == "" || r.TargetKey == "" || !slices.Contains([]string{knowledge.KindEffect, knowledge.KindRelation}, r.Kind) || !slices.Contains([]string{knowledge.OutcomeApplied, knowledge.OutcomeRedundant, knowledge.OutcomeStale}, r.Outcome) || (r.Outcome == knowledge.OutcomeStale && !validReason) || (r.Outcome != knowledge.OutcomeStale && reasonPresent) {
			return nil, fmt.Errorf("malformed knowledge outcome")
		}
		if _, ok := rows[r.ID]; ok {
			return nil, fmt.Errorf("duplicate knowledge outcome %q", r.ID)
		}
		rows[r.ID] = r
	}
	if tok, err = d.Token(); err != nil || tok != json.Delim(']') {
		return nil, fmt.Errorf("invalid knowledge outcomes")
	}
	if _, err = d.Token(); err != io.EOF {
		return nil, fmt.Errorf("trailing knowledge outcomes")
	}
	return rows, nil
}
