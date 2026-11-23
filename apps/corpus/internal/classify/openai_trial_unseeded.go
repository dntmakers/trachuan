package classify

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"sort"

	"danny.vn/trachuan/apps/corpus/internal/artifact"
	"danny.vn/trachuan/apps/corpus/internal/taxonomy"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func UnseededSelection(country, release string, input []taxonomy.LabelArticle, part, total int) (taxonomy.Labels, string, error) {
	labels := taxonomy.Labels{Country: country, Release: release, DrawRule: "unseeded", Articles: []taxonomy.LabelArticle{}}
	if part < 1 || total < 1 || part > total || total > 64 {
		return labels, "", fmt.Errorf("invalid unseeded part")
	}
	for _, a := range input {
		sum := sha256.Sum256([]byte(a.Key))
		if int(binary.BigEndian.Uint64(sum[:8])%uint64(total))+1 == part {
			labels.Articles = append(labels.Articles, a)
		}
	}
	sort.Slice(labels.Articles, func(i, j int) bool {
		a, b := labels.Articles[i], labels.Articles[j]
		if a.Key != b.Key {
			return a.Key < b.Key
		}
		return a.Anchor < b.Anchor
	})
	raw, err := json.Marshal(labels)
	if err != nil {
		return labels, "", err
	}
	raw = append(raw, []byte(fmt.Sprintf("unseeded:%d/%d", part, total))...)
	return labels, digest(raw), nil
}

func LoadUnseededSelection(ctx context.Context, db interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}, country, release string, part, total int) (taxonomy.Labels, string, error) {
	rows, err := db.Query(ctx, `SELECT d.key,p.anchor,p.text FROM document d JOIN provision p ON p.document_id=d.id WHERE d.class='primary' AND p.kind='dieu' AND NOT EXISTS (SELECT 1 FROM taxonomy_assignment a WHERE a.provision_id=p.id AND a.method IN ('phapdien_seed','inherited','manual')) ORDER BY d.key,p.anchor`)
	if err != nil {
		return taxonomy.Labels{}, "", fmt.Errorf("read unseeded articles: %w", err)
	}
	defer rows.Close()
	var articles []taxonomy.LabelArticle
	for rows.Next() {
		var a taxonomy.LabelArticle
		var text string
		if err = rows.Scan(&a.Key, &a.Anchor, &text); err != nil {
			return taxonomy.Labels{}, "", fmt.Errorf("decode unseeded article: %w", err)
		}
		if !taxonomy.IsFragment(text) {
			a.OwnTextSHA256 = taxonomy.OwnTextSHA256(text)
			articles = append(articles, a)
		}
	}
	if err = rows.Err(); err != nil {
		return taxonomy.Labels{}, "", fmt.Errorf("read unseeded articles: %w", err)
	}
	return UnseededSelection(country, release, articles, part, total)
}

func LoadUnseededDocuments(ctx context.Context, release, workshop *pgxpool.Pool, store artifact.Store, labels taxonomy.Labels) ([]Document, error) {
	nodes, err := LoadNodes(ctx, release)
	if err != nil {
		return nil, err
	}
	articles, err := LoadArticles(ctx, release, labels.Articles)
	if err != nil {
		return nil, err
	}
	if err = ValidateSelection(labels, articles, nodes); err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(labels.Articles))
	for _, a := range labels.Articles {
		keys = append(keys, a.Key)
	}
	rows, err := release.Query(ctx, `SELECT key,title,fetch_sha256 FROM document WHERE class='primary' AND key=ANY($1) ORDER BY key`, keys)
	if err != nil {
		return nil, fmt.Errorf("read selected classification documents: %w", err)
	}
	var primary []Document
	hashes := map[string]string{}
	for rows.Next() {
		var d Document
		var hash string
		if err = rows.Scan(&d.Key, &d.Title, &hash); err != nil {
			rows.Close()
			return nil, err
		}
		primary = append(primary, d)
		hashes[d.Key] = hash
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, err
	}
	selected, err := SelectLabelDocuments(labels.Articles, primary)
	if err != nil {
		return nil, err
	}
	return loadDocumentEvidence(ctx, release, workshop, store, selected, hashes)
}

func UnseededBuildHash(selection, input, recipe string) (string, error) {
	if !validKey(selection) || !validKey(input) || !validKey(recipe) {
		return "", fmt.Errorf("invalid unseeded build identity")
	}
	raw, err := json.Marshal([3]string{selection, input, recipe})
	if err != nil {
		return "", err
	}
	return digest(raw), nil
}
