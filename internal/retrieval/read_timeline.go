package retrieval

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type TimelineEvent struct {
	Kind   string          `json:"kind"`
	Date   *Date           `json:"date,omitempty"`
	Actor  *TimelineActor  `json:"actor,omitempty"`
	Target *TimelineTarget `json:"target,omitempty"`
}

type TimelineActor struct {
	Key           string `json:"key"`
	Number        string `json:"number,omitempty"`
	Issued        *Date  `json:"issued,omitempty"`
	EffectiveFrom *Date  `json:"effective_from,omitempty"`
	EffectiveTo   *Date  `json:"effective_to,omitempty"`
	InRelease     bool   `json:"in_release"`
}

type TimelineTarget struct {
	Scope    string  `json:"scope"`
	Anchor   *string `json:"anchor,omitempty"`
	Citation *string `json:"citation,omitempty"`
	Named    string  `json:"named,omitempty"`
}

const timelineSQL = `WITH events AS (
 SELECT dates.kind, dates.date, dates.rank, ''::text AS actor_key, ''::text AS actor_number,
 NULL::date AS issued, NULL::date AS eff_from, NULL::date AS eff_to, false AS in_release,
 NULL::bigint AS target_provision_id, NULL::text AS target_node_id, ''::text AS method,
 ''::text AS source_key, ''::text AS artifact_sha256, 0 AS ordinal
 FROM document d CROSS JOIN LATERAL (VALUES
 ('issued',d.issue_date,0),('effective_from',d.eff_from,1),('effective_to',d.eff_to,2)
 ) dates(kind,date,rank) WHERE d.id=$1 AND dates.date IS NOT NULL
 UNION ALL
 SELECT r.kind, a.issue_date, 3+array_position($2::text[],r.kind),
 COALESCE(a.key,r.source_key), COALESCE(a.number,r.source_number), a.issue_date,a.eff_from,a.eff_to,
 a.id IS NOT NULL, r.target_provision_id,r.target_node_id,r.method,r.source_key,r.artifact_sha256,r.ordinal
 FROM relation r LEFT JOIN document a ON a.id=r.source_document_id
 WHERE r.target_document_id=$1 AND r.kind=ANY($2::text[])
 ) SELECT kind,date,actor_key,actor_number,issued,eff_from,eff_to,in_release,
 target_provision_id,target_node_id,method FROM events
 ORDER BY date ASC NULLS LAST,rank,source_key COLLATE "C",artifact_sha256 COLLATE "C",ordinal
 LIMIT $3 OFFSET $4`

type timelineAncestry struct {
	anchor string
	nodes  []Provision
}

func timelineAncestries(ctx context.Context, tx pgx.Tx, documentID int64, ids []int64) (map[int64]timelineAncestry, error) {
	result := make(map[int64]timelineAncestry)
	if len(ids) == 0 {
		return result, nil
	}
	rows, err := tx.Query(ctx, `WITH RECURSIVE ancestry AS (
 SELECT id AS target_id,parent_id,anchor AS target_anchor,kind,label,0 AS depth
 FROM provision WHERE document_id=$1 AND id=ANY($2::bigint[])
 UNION ALL
 SELECT a.target_id,p.parent_id,a.target_anchor,p.kind,p.label,a.depth+1
 FROM ancestry a JOIN provision p ON p.id=a.parent_id AND p.document_id=$1
 ) SELECT target_id,target_anchor,kind,label FROM ancestry ORDER BY target_id,depth DESC`, documentID, ids)
	if err != nil {
		return nil, databaseFailure("query timeline ancestry", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var anchor string
		var p Provision
		if err = rows.Scan(&id, &anchor, &p.Kind, &p.Label); err != nil {
			return nil, databaseFailure("scan timeline ancestry", err)
		}
		item := result[id]
		item.anchor = anchor
		item.nodes = append(item.nodes, p)
		result[id] = item
	}
	if err = rows.Err(); err != nil {
		return nil, databaseFailure("read timeline ancestry", err)
	}
	return result, nil
}

func readTimelineScope(ctx context.Context, tx pgx.Tx, c cursor) (readSequence, string, error) {
	key, _, _ := ParseHandle(c.Item.Handle)
	var id int64
	err := tx.QueryRow(ctx, `SELECT id FROM document WHERE key=$1`, key).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return readSequence{doc: key}, "not_found", nil
	}
	if err != nil {
		return readSequence{}, "", databaseFailure("query timeline document", err)
	}
	seq, err := readTimeline(ctx, tx, id, key, c.Unit)
	return seq, "", err
}

func readTimeline(ctx context.Context, tx pgx.Tx, id int64, key string, offset int) (readSequence, error) {
	seq := readSequence{doc: key, offset: offset, complete: true}
	rows, err := tx.Query(ctx, timelineSQL, id, pointerKinds()[:5], RelationPageSize+1, offset)
	if err != nil {
		return seq, databaseFailure("query timeline", err)
	}
	defer rows.Close()
	var targets []*int64
	var nodes []*string
	var methods []string
	for rows.Next() {
		var event TimelineEvent
		var actor TimelineActor
		var date, issued, from, to *time.Time
		var target *int64
		var node *string
		var method string
		if err = rows.Scan(&event.Kind, &date, &actor.Key, &actor.Number, &issued, &from, &to, &actor.InRelease, &target, &node, &method); err != nil {
			return seq, databaseFailure("scan timeline", err)
		}
		if len(seq.units) == RelationPageSize {
			seq.more = true
			break
		}
		event.Date = dateValue(date)
		if event.Kind != "issued" && event.Kind != "effective_from" && event.Kind != "effective_to" {
			actor.Number = presentMetadata(actor.Number)
			actor.Issued, actor.EffectiveFrom, actor.EffectiveTo = dateValue(issued), dateValue(from), dateValue(to)
			event.Actor = &actor
			event.Target = &TimelineTarget{Scope: "document"}
		}
		seq.units = append(seq.units, Unit{Kind: "timeline", Event: &event})
		targets, nodes, methods = append(targets, target), append(nodes, node), append(methods, method)
	}
	if err = rows.Err(); err != nil {
		return seq, databaseFailure("read timeline", err)
	}
	rows.Close()
	ids := make([]int64, 0, len(targets))
	for _, target := range targets {
		if target != nil {
			ids = append(ids, *target)
		}
	}
	ancestries, err := timelineAncestries(ctx, tx, id, ids)
	if err != nil {
		return seq, err
	}
	for i := range seq.units {
		event := seq.units[i].Event
		if event.Actor == nil {
			continue
		}
		textRule := false
		for _, method := range strings.Split(methods[i], ",") {
			textRule = textRule || method == "text_rule/1" || method == "reviewed"
		}
		if targets[i] == nil && nodes[i] == nil {
			continue
		}
		var ancestry timelineAncestry
		var found bool
		if targets[i] != nil {
			ancestry, found = ancestries[*targets[i]]
		}
		if found && (!textRule || nodes[i] == nil || *nodes[i] == ancestry.anchor) {
			anchor := ancestry.anchor
			cite := citation(ancestry.nodes)
			event.Target = &TimelineTarget{Scope: "provision", Anchor: &anchor, Citation: &cite}
			continue
		}
		event.Target = &TimelineTarget{Scope: "unresolved_provision"}
		if textRule && nodes[i] != nil {
			event.Target.Named = *nodes[i]
		}
	}
	return seq, nil
}
