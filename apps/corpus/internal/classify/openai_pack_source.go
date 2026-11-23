package classify

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"

	"danny.vn/trachuan/apps/corpus/internal/evaluation"
	"danny.vn/trachuan/apps/corpus/internal/taxonomy"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type packQueryer interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

type PackBuild struct {
	Country, Release, InputHash, RecipeHash, Taxonomy string
	Articles                                          []taxonomy.LabelArticle
	Nodes                                             []Node
	Menu                                              []Family
}

type PackBaseline struct{ references []packReference }

type packReference struct {
	name, provider, docDir, articleDir string
	selection                          packSelection
}

func LoadOpenAIPackSource(ctx context.Context, db *pgxpool.Pool, country, release string) (PackBuild, error) {
	tx, err := db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return PackBuild{}, fmt.Errorf("open pack source snapshot: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	b, err := readPackBuild(ctx, tx, country, release)
	if err != nil {
		return b, err
	}
	labels, _, err := LoadUnseededSelection(ctx, tx, country, release, 1, 1)
	if err != nil {
		return b, err
	}
	b.Articles = labels.Articles
	return b, tx.Commit(ctx)
}

func LoadOpenAIPackBaseline(ctx context.Context, db *pgxpool.Pool, root string) (PackBaseline, error) {
	var baseline PackBaseline
	tx, err := db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return baseline, fmt.Errorf("open pack baseline snapshot: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	b, err := readPackBuild(ctx, tx, "vn", PackBaselineRelease)
	if err != nil {
		return baseline, err
	}
	baseline.references, err = readPackReferences(root, b)
	if err != nil {
		return baseline, err
	}
	if err = verifyPackReferenceTriples(ctx, tx, b.Nodes, baseline.references); err != nil {
		return baseline, err
	}
	return baseline, tx.Commit(ctx)
}

func readPackBuild(ctx context.Context, tx pgx.Tx, country, release string) (PackBuild, error) {
	b := PackBuild{Country: country, Release: release}
	if err := tx.QueryRow(ctx, `SELECT build_input_hash,build_recipe_hash,taxonomy_version FROM release WHERE singleton AND country=$1 AND rel=$2`, country, release).Scan(&b.InputHash, &b.RecipeHash, &b.Taxonomy); err != nil {
		return b, fmt.Errorf("read pack release identity: %w", err)
	}
	if !validKey(b.InputHash) || !validKey(b.RecipeHash) || !validKey(b.Taxonomy) {
		return b, fmt.Errorf("invalid pack release build identity")
	}
	var err error
	if b.Nodes, err = loadNodes(ctx, tx); err != nil {
		return b, err
	}
	b.Menu, err = loadFamilies(ctx, tx)
	return b, err
}

func readPackReferences(root string, b PackBuild) ([]packReference, error) {
	raw, err := evaluation.VNTaxonomyReferences()
	if err != nil {
		return nil, err
	}
	var refs []packReference
	for i, descriptor := range [...]struct{ stem, slice, provider string }{{"labels", "seeded", "package"}, {"multi-family-anthropic", "multi-family", "anthropic"}, {"post-2023-anthropic", "post-2023", "anthropic"}} {
		slice, provider, labels, err := taxonomy.VerifyScoringInput("openai", raw[i])
		if err != nil || slice != descriptor.slice || provider != descriptor.provider || labels.Country != "vn" || labels.Release != PackBaselineRelease || labels.Country != b.Country || labels.Release != b.Release || len(labels.Articles) == 0 {
			return nil, fmt.Errorf("invalid pinned baseline reference %s", descriptor.stem)
		}
		stem := filepath.Join(root, "gpt-6.2-sol-"+descriptor.stem)
		sel := packSelection{build: b, articles: labels.Articles, hash: digest(raw[i])}
		refs = append(refs, packReference{slice, provider, stem + "-documents", stem + "-articles", sel})
	}
	return refs, nil
}

func verifyPackReferenceTriples(ctx context.Context, db packQueryer, nodes []Node, refs []packReference) error {
	active := make(map[string]bool, len(nodes))
	for _, n := range nodes {
		active[n.Code] = n.Active
	}
	for _, ref := range refs {
		labels := make(map[packArticleKey]taxonomy.LabelArticle, len(ref.selection.articles))
		for _, a := range ref.selection.articles {
			key := packArticleKey{a.Key, a.Anchor}
			if _, exists := labels[key]; exists || key.document == "" || key.anchor == "" || !validKey(a.OwnTextSHA256) || len(a.Codes) == 0 {
				return fmt.Errorf("invalid baseline %s reference triple", ref.name)
			}
			for _, code := range a.Codes {
				if !active[code] {
					return fmt.Errorf("baseline %s reference code %s is not active", ref.name, code)
				}
			}
			labels[key] = a
		}
		if err := matchPackReferenceTriples(ctx, db, ref, labels); err != nil {
			return err
		}
	}
	return nil
}

func matchPackReferenceTriples(ctx context.Context, db packQueryer, ref packReference, labels map[packArticleKey]taxonomy.LabelArticle) error {
	rows, err := db.Query(ctx, `SELECT d.key,p.anchor,p.text FROM document d JOIN provision p ON p.document_id=d.id WHERE d.class='primary' AND p.kind='dieu' AND d.key=ANY($1)`, ref.selection.documentKeys())
	if err != nil {
		return fmt.Errorf("read baseline %s articles: %w", ref.name, err)
	}
	defer rows.Close()
	for rows.Next() {
		var key packArticleKey
		var text string
		if err = rows.Scan(&key.document, &key.anchor, &text); err != nil {
			return fmt.Errorf("decode baseline %s article: %w", ref.name, err)
		}
		if a, found := labels[key]; found {
			if taxonomy.OwnTextSHA256(text) != a.OwnTextSHA256 {
				return fmt.Errorf("baseline %s own_text_sha256 differs from %s %s", ref.name, key.document, key.anchor)
			}
			delete(labels, key)
		}
	}
	if err = rows.Err(); err != nil {
		return fmt.Errorf("read baseline %s articles: %w", ref.name, err)
	}
	if len(labels) != 0 {
		missing := make([]string, 0, len(labels))
		for key := range labels {
			missing = append(missing, key.document+" "+key.anchor)
		}
		sort.Strings(missing)
		return fmt.Errorf("baseline %s is missing reference article %s", ref.name, missing[0])
	}
	return nil
}

func makePackSelection(b PackBuild, part, total int) (packSelection, error) {
	labels, hash, err := UnseededSelection(b.Country, b.Release, b.Articles, part, total)
	if err != nil {
		return packSelection{}, err
	}
	bound, err := UnseededBuildHash(hash, b.InputHash, b.RecipeHash)
	return packSelection{b, labels.Articles, bound, fmt.Sprintf("unseeded:%d/%d", part, total)}, err
}
