package eval

import (
	"context"
	"fmt"

	"danny.vn/trachuan/internal/retrieval"
	"github.com/jackc/pgx/v5"
)

type warningQuery interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func warningScope(ctx context.Context, db warningQuery, target, carrier string) (bool, error) {
	targetKey, targetAnchor, err := retrieval.ParseHandle(target)
	if err != nil || targetAnchor == "" {
		return false, nil
	}
	carrierKey, carrierAnchor, err := retrieval.ParseHandle(carrier)
	if err != nil || carrierAnchor == "" || targetKey != carrierKey {
		return false, nil
	}
	const query = `WITH RECURSIVE selected AS (
 SELECT p.id,p.parent_id,p.document_id,p.anchor FROM provision p JOIN document d ON d.id=p.document_id WHERE d.key=$1 AND p.anchor IN ($2,$3)
), ancestors AS (
 SELECT id AS origin,id,parent_id,document_id FROM selected
 UNION
 SELECT a.origin,p.id,p.parent_id,p.document_id FROM ancestors a JOIN provision p ON p.id=a.parent_id AND p.document_id=a.document_id
)
SELECT (SELECT count(*) FROM selected WHERE anchor=$2), (SELECT count(*) FROM selected WHERE anchor=$3),
 EXISTS (SELECT 1 FROM ancestors a JOIN selected s ON s.id=a.origin JOIN selected t ON t.id=a.id WHERE (s.anchor=$2 AND t.anchor=$3) OR (s.anchor=$3 AND t.anchor=$2))`
	var targets, carriers int
	var related bool
	if err = db.QueryRow(ctx, query, targetKey, targetAnchor, carrierAnchor).Scan(&targets, &carriers, &related); err != nil {
		return false, fmt.Errorf("query warning provision scope: %w", err)
	}
	if targets > 1 || carriers > 1 {
		return false, fmt.Errorf("ambiguous warning provision")
	}
	return targets == 1 && carriers == 1 && related, nil
}
