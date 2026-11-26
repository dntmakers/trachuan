package releasechange

import (
	"context"
	"encoding/json"
	"fmt"
)

const summaryDocumentsSQL = `SELECT d.key, d.number_norm, d.status_class, to_char(d.eff_from,'YYYY-MM-DD'), to_char(d.eff_to,'YYYY-MM-DD'), d.is_current, d.validity_conflict, d.text_source, d.text_complete,
 EXISTS (SELECT 1 FROM coverage_gap g WHERE g.stage='build' AND g.kind='baseline_served' AND g.item_key=d.key)
 FROM document d ORDER BY d.key`

const summaryProvisionsSQL = `SELECT d.key, p.id, p.parent_id, p.anchor, p.text,
 COALESCE((SELECT jsonb_agg(jsonb_build_object('node',t.node_id,'family',t.family_id,'method',t.method,'exclude',t.exclude)) FROM taxonomy_assignment t WHERE t.provision_id=p.id),'[]'::jsonb),
 COALESCE((SELECT jsonb_agg(jsonb_build_object('status_class',v.status_class,'eff_from',to_char(v.eff_from,'YYYY-MM-DD'),'eff_to',to_char(v.eff_to,'YYYY-MM-DD'),'method',v.method)) FROM provision_validity v WHERE v.provision_id=p.id),'[]'::jsonb)
 FROM provision p JOIN document d ON d.id=p.document_id ORDER BY d.key, p.anchor`

const summaryRelationsSQL = `SELECT r.kind, r.method, r.source_key, r.source_number, r.source_document_id IS NOT NULL, sp.anchor,
 r.target_key, r.target_number, r.target_document_id IS NOT NULL, tp.anchor, r.target_node_id
 FROM relation r LEFT JOIN provision sp ON sp.id=r.source_provision_id LEFT JOIN provision tp ON tp.id=r.target_provision_id`

func (s SQLRelease) SummaryDocuments(ctx context.Context) ([]SummaryDocument, error) {
	rows, err := s.q.Query(ctx, summaryDocumentsSQL)
	if err != nil {
		return nil, fmt.Errorf("read summary documents: %w", err)
	}
	defer rows.Close()
	var out []SummaryDocument
	for rows.Next() {
		var d SummaryDocument
		if err = rows.Scan(&d.Key, &d.NumberNorm, &d.StatusClass, &d.EffFrom, &d.EffTo, &d.IsCurrent, &d.ValidityConflict, &d.TextSource, &d.TextComplete, &d.Baseline); err != nil {
			return nil, fmt.Errorf("read summary documents: %w", err)
		}
		out = append(out, d)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("read summary documents: %w", err)
	}
	return out, nil
}

func (s SQLRelease) SummaryNodes(ctx context.Context) (map[int64]TaxonomyNode, error) {
	rows, err := s.q.Query(ctx, `SELECT id, code, status='active' AND provision_count>0 FROM taxonomy_node`)
	if err != nil {
		return nil, fmt.Errorf("read taxonomy nodes: %w", err)
	}
	defer rows.Close()
	out := map[int64]TaxonomyNode{}
	for rows.Next() {
		var id int64
		var n TaxonomyNode
		if err = rows.Scan(&id, &n.Code, &n.Served); err != nil {
			return nil, fmt.Errorf("read taxonomy nodes: %w", err)
		}
		out[id] = n
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("read taxonomy nodes: %w", err)
	}
	return out, nil
}

func (s SQLRelease) SummaryProvisions(ctx context.Context, fn func(string, []SummaryProvision) error) error {
	rows, err := s.q.Query(ctx, summaryProvisionsSQL)
	if err != nil {
		return fmt.Errorf("read summary provisions: %w", err)
	}
	defer rows.Close()
	current := ""
	var group []SummaryProvision
	for rows.Next() {
		var key, text string
		var p SummaryProvision
		var assignments, validity []byte
		if err = rows.Scan(&key, &p.ID, &p.ParentID, &p.Anchor, &text, &assignments, &validity); err != nil {
			return fmt.Errorf("read summary provisions: %w", err)
		}
		if err = json.Unmarshal(assignments, &p.Assignments); err != nil {
			return refuse("decode taxonomy assignments of %s %s: %v", key, p.Anchor, err)
		}
		if err = json.Unmarshal(validity, &p.Validity); err != nil {
			return refuse("decode provision validity of %s %s: %v", key, p.Anchor, err)
		}
		p.TextSHA256 = hashPrefix + sha(text)
		if key != current && group != nil {
			if err = fn(current, group); err != nil {
				return err
			}
			group = nil
		}
		current = key
		group = append(group, p)
	}
	if err = rows.Err(); err != nil {
		return fmt.Errorf("read summary provisions: %w", err)
	}
	if group != nil {
		return fn(current, group)
	}
	return nil
}

func (s SQLRelease) SummaryRelations(ctx context.Context, fn func(SummaryRelation) error) error {
	rows, err := s.q.Query(ctx, summaryRelationsSQL)
	if err != nil {
		return fmt.Errorf("read summary relations: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var r SummaryRelation
		if err = rows.Scan(&r.Kind, &r.Method, &r.SourceKey, &r.SourceNumber, &r.SourceInRelease, &r.SourceAnchor, &r.TargetKey, &r.TargetNumber, &r.TargetInRelease, &r.TargetAnchor, &r.TargetNode); err != nil {
			return fmt.Errorf("read summary relations: %w", err)
		}
		if err = fn(r); err != nil {
			return err
		}
	}
	if err = rows.Err(); err != nil {
		return fmt.Errorf("read summary relations: %w", err)
	}
	return nil
}
