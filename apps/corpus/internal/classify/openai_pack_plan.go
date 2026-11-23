package classify

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
)

type packMenuEntry struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

type packNodeEntry struct {
	Code   string `json:"code"`
	Name   string `json:"name"`
	Parent string `json:"parent_code"`
}

func verifyPackRequest(part openAITrialItem, r Recipe, item Item, families []Family) error {
	stage := "document"
	if r.Article != nil {
		stage = "article"
	}
	bad := func(field string) error {
		return fmt.Errorf("input.jsonl custom_id %s %s does not match verified %s request", part.ID, field, stage)
	}
	var actual openAIRequest
	if packCheckJSON(part.Body) != nil || json.Unmarshal(part.Body, &actual) != nil || len(actual.Input) != 2 || len(actual.Input[0].Content) != 2 || len(actual.Input[1].Content) != 1 {
		return bad("body or messages")
	}
	menuText := actual.Input[0].Content[1].Text
	payloadText := actual.Input[1].Content[0].Text
	payloadRaw := []byte(payloadText)
	var instruction string
	var schema map[string]any
	var menu any
	if r.Article == nil {
		instruction, schema = documentRequestContract(r.F)
		entries := make([]packMenuEntry, 0, len(families))
		for _, f := range families {
			entries = append(entries, packMenuEntry{f.Code, f.ParentName + " > " + f.Name})
		}
		menu = struct {
			Families []packMenuEntry `json:"active_level_2_nodes"`
		}{entries}
		var payload struct {
			Document Document `json:"document"`
		}
		if decodePackJSON("input.jsonl document payload", payloadRaw, &payload) != nil || !packSameRecord(payloadRaw, payload) || payload.Document.Key != item.Key {
			return bad("document payload")
		}
	} else {
		req := item.Article
		var err error
		instruction, schema, err = articleRequestContract(r.Article.Step, req.Articles)
		if err != nil {
			return bad("article schema")
		}
		entries := make([]packNodeEntry, 0, len(req.Nodes))
		for _, n := range req.Nodes {
			entries = append(entries, packNodeEntry{n.Code, n.Name, n.Parent})
		}
		menu = struct {
			Nodes []packNodeEntry `json:"allowed_subtrees"`
		}{entries}
		var payload struct {
			Key      string    `json:"document_key"`
			Articles []Article `json:"articles"`
		}
		articles := slices.Clone(req.Articles)
		for i := range articles {
			articles[i].ChapterHeading = ""
		}
		if decodePackJSON("input.jsonl article payload", payloadRaw, &payload) != nil || !packSameRecord(payloadRaw, payload) || payload.Key != req.DocumentKey || !slices.Equal(payload.Articles, articles) {
			return bad("article payload")
		}
	}
	if packCheckJSON([]byte(menuText)) != nil || !packSameRecord([]byte(menuText), menu) {
		return bad("developer menu")
	}
	expected := openAIRequestContract(r, stage+"_classification", instruction, menuText, payloadText, schema, r.outputTokens())
	if !packSameRecord(part.Body, expected) {
		return bad("body, instruction or schema")
	}
	return nil
}

func verifyPackDocuments(t *openAITrial, selection packSelection) error {
	items := t.result.Items
	keys := selection.documentKeys()
	if len(items) != len(keys) || len(keys) == 0 || items[0].Selection == nil || items[0].Selection.LabelsHash != selection.hash || !slices.Equal(items[0].Selection.DocumentKeys, keys) {
		return fmt.Errorf("plan.json document_selection does not account for source documents")
	}
	if len(t.nodes) != 0 || !slices.Equal(t.result.Families, selection.build.Menu) || len(t.result.Families) == 0 {
		return fmt.Errorf("plan.json families do not match the ordered source menu")
	}
	for i, item := range items {
		if item.Key != keys[i] || item.Article != nil || item.ArticleMeta != nil || i > 0 && item.Selection != nil || !validKey(item.RequestHash) || item.InputTokens <= 0 || item.Local != (item.InputTokens > t.o.Recipe.InputBound) || item.LocalReason != "" {
			return fmt.Errorf("plan.json document item %d does not match selection or local disposition", i)
		}
	}
	for _, part := range t.parts {
		if err := verifyPackRequest(part, t.o.Recipe, items[part.Index], t.result.Families); err != nil {
			return err
		}
	}
	return nil
}

type packArticleKey struct{ document, anchor string }

func verifyPackArticles(t *openAITrial, sel packSelection, families map[string]Outcome) error {
	if !slices.Equal(t.nodes, sel.build.Nodes) || len(t.result.Families) != 0 {
		return fmt.Errorf("plan.json article taxonomy nodes differ from source")
	}
	remaining := make(map[packArticleKey]bool, len(sel.articles))
	for _, a := range sel.articles {
		key := packArticleKey{a.Key, a.Anchor}
		if remaining[key] || a.Key == "" || a.Anchor == "" || !validKey(a.OwnTextSHA256) {
			return fmt.Errorf("invalid or duplicate selected article triple")
		}
		remaining[key] = true
	}
	items := t.result.Items
	meta := items[0].ArticleMeta
	if meta == nil || meta.LabelsHash != sel.hash || meta.InputHash != "" || len(meta.CountedRequests) != 0 {
		return fmt.Errorf("plan.json article_metadata does not match selection")
	}
	for _, skip := range meta.Skipped {
		key := packArticleKey{skip.DocumentKey, skip.Anchor}
		family := families[skip.DocumentKey]
		reason := family.Outcome
		if reason == "ok" {
			reason = "too_long"
		}
		if !remaining[key] || !packEqualJSON(skip.Outcome, emptyArticle(reason)) {
			return fmt.Errorf("plan.json article_metadata.skipped does not match selection or document outcome")
		}
		delete(remaining, key)
	}
	for i, item := range items {
		if item.Selection != nil || item.LocalReason != "" || i > 0 && item.ArticleMeta != nil {
			return fmt.Errorf("invalid plan.json article item %d metadata", i)
		}
		if item.Local {
			if len(items) != 1 || item.Article != nil || item.Key != "" || item.RequestHash != "" || item.InputTokens != 0 {
				return fmt.Errorf("invalid local-only plan.json article item")
			}
			continue
		}
		req := item.Article
		if req == nil || item.Key != strconv.Itoa(i) || req.InputTokens != item.InputTokens || len(req.Articles) == 0 || req.Step != 0 || req.Family != "" || req.DocumentTitle != "" || len(req.DocumentCodes) != 0 || req.PromptVersion != 0 {
			return fmt.Errorf("invalid plan.json article item %d request", i)
		}
		family := families[req.DocumentKey]
		menu, err := allowedNodes(family.Codes, t.nodes)
		if err != nil || family.Outcome != "ok" || len(menu) == 0 || !slices.Equal(req.Nodes, menu) {
			return fmt.Errorf("plan.json article item %d allowed_subtrees differ from document outcome", i)
		}
		for _, article := range req.Articles {
			key := packArticleKey{req.DocumentKey, article.Anchor}
			if !remaining[key] {
				return fmt.Errorf("plan.json article item %d repeats or adds an unselected anchor", i)
			}
			delete(remaining, key)
		}
	}
	if len(remaining) > 0 {
		return fmt.Errorf("plan.json does not cover all selected anchors")
	}
	for _, part := range t.parts {
		if err := verifyPackRequest(part, t.o.Recipe, items[part.Index], nil); err != nil {
			return err
		}
	}
	return nil
}
