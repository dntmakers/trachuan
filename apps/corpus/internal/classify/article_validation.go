package classify

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"

	"danny.vn/trachuan/apps/corpus/internal/batchapi"
)

func emptyArticle(outcome string) ArticleOutcome {
	return ArticleOutcome{Outcome: outcome, Codes: []CodeConfidence{}, Dropped: []string{}}
}

func readObject(b []byte) (map[string]json.RawMessage, error) {
	return batchapi.DecodeObject(b)
}

func closedObject(b []byte, keys []string) (map[string]json.RawMessage, error) {
	out, err := readObject(b)
	if err != nil {
		return nil, err
	}
	if len(out) != len(keys) {
		return nil, fmt.Errorf("unexpected JSON fields")
	}
	for _, key := range keys {
		if out[key] == nil {
			return nil, fmt.Errorf("missing JSON field")
		}
	}
	return out, nil
}

func ValidateArticles(stop, text string, req ArticleRequest, nodes []Node) (map[string]ArticleOutcome, int) {
	invalid := map[string]ArticleOutcome{}
	dropped := 0
	keys := make([]string, 0, len(req.Articles))
	for _, a := range req.Articles {
		invalid[a.Anchor] = emptyArticle("invalid_response")
		keys = append(keys, a.Anchor)
	}
	if stop != "end_turn" {
		return invalid, dropped
	}
	raw, err := readObject([]byte(text))
	if err != nil {
		return invalid, dropped
	}
	for anchor := range raw {
		if _, requested := invalid[anchor]; !requested {
			delete(raw, anchor)
			dropped++
		}
	}
	active := map[string]bool{}
	var subtree map[string]bool
	if req.Step == 2 {
		subtree = map[string]bool{}
		menu, menuErr := allowedNodes([]string{req.Family}, nodes)
		if menuErr != nil {
			return invalid, dropped
		}
		for _, n := range menu {
			subtree[n.Code] = true
		}
	}
	for _, n := range nodes {
		if n.Active && n.Level >= 2 && n.Level <= 4 && (n.Origin == "phapdien" || n.Origin == "custom") && (req.Step != 1 || n.Level == 2) && (req.Step != 2 || subtree[n.Code]) {
			active[n.Code] = true
		}
	}
	allowed := map[string]bool{}
	for _, n := range req.Nodes {
		allowed[n.Code] = active[n.Code]
	}
	result := map[string]ArticleOutcome{}
	for _, anchor := range keys {
		if _, present := raw[anchor]; !present {
			result[anchor] = invalid[anchor]
			continue
		}
		obj, parseErr := closedObject(raw[anchor], []string{"codes"})
		if parseErr != nil {
			return invalid, dropped
		}
		var codes []json.RawMessage
		if len(obj["codes"]) == 0 || obj["codes"][0] != '[' || json.Unmarshal(obj["codes"], &codes) != nil {
			return invalid, dropped
		}
		out := emptyArticle("ok")
		best := map[string]float64{}
		badConfidence := false
		for _, v := range codes {
			fields, fieldErr := closedObject(v, []string{"code", "confidence"})
			if fieldErr != nil {
				return invalid, dropped
			}
			var code string
			if len(fields["code"]) == 0 || fields["code"][0] != '"' || json.Unmarshal(fields["code"], &code) != nil {
				return invalid, dropped
			}
			rawConfidence := fields["confidence"]
			if len(rawConfidence) == 0 || (rawConfidence[0] != '-' && (rawConfidence[0] < '0' || rawConfidence[0] > '9')) {
				return invalid, dropped
			}
			confidence, floatErr := strconv.ParseFloat(string(rawConfidence), 64)
			if floatErr != nil || math.IsNaN(confidence) || math.IsInf(confidence, 0) || confidence < 0 || confidence > 1 {
				badConfidence = true
				continue
			}
			if !allowed[code] {
				out.Dropped = append(out.Dropped, code)
				continue
			}
			prior, exists := best[code]
			if !exists || confidence > prior {
				best[code] = confidence
			}
		}
		if badConfidence {
			result[anchor] = emptyArticle("invalid_response")
			continue
		}
		for code, confidence := range best {
			out.Codes = append(out.Codes, CodeConfidence{code, confidence})
		}
		sort.Slice(out.Codes, func(i, j int) bool { return out.Codes[i].Code < out.Codes[j].Code })
		result[anchor] = out
	}
	return result, dropped
}
