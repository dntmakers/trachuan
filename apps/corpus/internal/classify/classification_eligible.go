package classify

import (
	"context"
	"fmt"

	"danny.vn/trachuan/apps/corpus/internal/taxonomy"
	"github.com/jackc/pgx/v5"
)

const classificationDocumentClass = "primary"
const classificationProvisionKind = "dieu"

func classificationEligible(class, kind, text string, covered bool) bool {
	return class == classificationDocumentClass && kind == classificationProvisionKind && !covered && !taxonomy.IsFragment(text)
}

func VisitEligibleArticles(ctx context.Context, db interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}, visit func(id int64, key, anchor, text string) error) error {
	const query = `SELECT p.id,d.key,p.anchor,p.text,d.class,p.kind,
 EXISTS (SELECT 1 FROM taxonomy_assignment a WHERE a.provision_id=p.id AND a.method IN ('phapdien_seed','inherited','manual'))
 FROM document d JOIN provision p ON p.document_id=d.id
 WHERE d.class=$1 AND p.kind=$2 ORDER BY d.key,p.anchor`
	rows, err := db.Query(ctx, query, classificationDocumentClass, classificationProvisionKind)
	if err != nil {
		return fmt.Errorf("query classification candidates: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var key, anchor, text, class, kind string
		var covered bool
		if err = rows.Scan(&id, &key, &anchor, &text, &class, &kind, &covered); err != nil {
			return fmt.Errorf("decode classification candidate: %w", err)
		}
		if classificationEligible(class, kind, text, covered) {
			if err = visit(id, key, anchor, text); err != nil {
				return err
			}
		}
	}
	if err = rows.Err(); err != nil {
		return fmt.Errorf("read classification candidates: %w", err)
	}
	return nil
}
