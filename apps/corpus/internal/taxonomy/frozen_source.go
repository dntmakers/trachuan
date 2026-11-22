package taxonomy

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"sort"
)

func ReadFrozenSource(path, slice, country, release string) (FrozenLabels, error) {
	var source FrozenLabels
	pins := map[string]string{"multi-family": "e46dc8ad6ec74fd341b0feeac5d7fbd4e5c7d2881fb758ce2f64c8b575ab3b04", "post-2023": "8d8d7ce12dc4a96338dc8553d56e4d8e9c5ccb0dfe25e8a1dc0c040918f4ce3c"}
	pin, ok := pins[slice]
	if !ok {
		return source, fmt.Errorf("no frozen source for slice %q", slice)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return source, fmt.Errorf("read frozen source: %w", err)
	}
	sum := sha256.Sum256(data)
	if fmt.Sprintf("%x", sum) != pin {
		return source, fmt.Errorf("frozen source hash mismatch for %s", slice)
	}
	if err = json.Unmarshal(data, &source); err != nil {
		return source, fmt.Errorf("decode frozen source: %w", err)
	}
	if source.Country != country || source.Release != release {
		return source, fmt.Errorf("frozen source release mismatch")
	}
	return source, nil
}

func PacketFromFrozen(source FrozenLabels, rows []DrawArticle, tree []PacketNode, slice string) (Packet, error) {
	byID := make(map[ArticleID]PacketArticle, len(rows))
	for _, row := range rows {
		id := ArticleID{row.Key, row.Anchor}
		if _, ok := byID[id]; ok {
			return Packet{}, fmt.Errorf("duplicate loaded article")
		}
		byID[id] = row.PacketArticle
	}
	articles := make([]PacketArticle, 0, len(source.Articles))
	seen := map[ArticleID]bool{}
	for _, entry := range source.Articles {
		id := ArticleID{entry.Key, entry.Anchor}
		row, ok := byID[id]
		if id.Key == "" || id.Anchor == "" || seen[id] || !ok || OwnTextSHA256(row.Text) != entry.OwnTextSHA256 {
			return Packet{}, fmt.Errorf("invalid frozen article %s %s", id.Key, id.Anchor)
		}
		seen[id] = true
		row.OwnTextSHA256 = entry.OwnTextSHA256
		articles = append(articles, row)
	}
	if len(articles) == 0 {
		return Packet{}, fmt.Errorf("empty frozen source")
	}
	return Packet{LabelingRule: LabelingRule, Country: source.Country, Release: source.Release, Slice: slice, Seed: source.Seed, DrawRule: source.DrawRule, Articles: articles, Tree: tree}, nil
}

func SourceListSHA256(articles []LabelArticle) (string, error) {
	type identity struct {
		Key    string `json:"document_key"`
		Anchor string `json:"anchor"`
		Hash   string `json:"own_text_sha256"`
	}
	ids := make([]identity, 0, len(articles))
	for _, a := range articles {
		ids = append(ids, identity{a.Key, a.Anchor, a.OwnTextSHA256})
	}
	sort.Slice(ids, func(i, j int) bool {
		if ids[i].Key == ids[j].Key {
			return ids[i].Anchor < ids[j].Anchor
		}
		return ids[i].Key < ids[j].Key
	})
	for i := 1; i < len(ids); i++ {
		if ids[i].Key == ids[i-1].Key && ids[i].Anchor == ids[i-1].Anchor {
			return "", fmt.Errorf("duplicate article identity")
		}
	}
	data, err := json.Marshal(ids)
	if err != nil {
		return "", fmt.Errorf("encode article identities: %w", err)
	}
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum), nil
}
