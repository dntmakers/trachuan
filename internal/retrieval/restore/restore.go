package restore

import (
	"context"
	"errors"
	"sort"
	"strings"
	"unicode"
)

type Candidate struct {
	Text      string
	PrefixCut bool
}

type pair struct{ left, right string }
type counts struct {
	singles map[string]int
	pairs   map[pair]int
	forms   map[string]map[string]struct{}
}

func Restore(ctx context.Context, query []byte, candidates []Candidate, optionCap int) ([]byte, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	if optionCap < 1 || optionCap > 8 {
		return nil, false, errors.New("restore option cap must be between 1 and 8")
	}
	if len(candidates) == 0 {
		return query, false, nil
	}
	c := counts{singles: make(map[string]int), pairs: make(map[pair]int), forms: make(map[string]map[string]struct{})}
	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}
		tokens := tokenize([]byte(candidate.Text))
		if candidate.PrefixCut && len(tokens) > 0 && tokens[len(tokens)-1].end == len(candidate.Text) {
			tokens = tokens[:len(tokens)-1]
		}
		for i, t := range tokens {
			c.singles[t.score]++
			if t.fold != "" {
				if c.forms[t.fold] == nil {
					c.forms[t.fold] = make(map[string]struct{})
				}
				c.forms[t.fold][t.score] = struct{}{}
			}
			if i > 0 && t.adjacent {
				c.pairs[pair{tokens[i-1].score, t.score}]++
			}
		}
	}
	tokens := tokenize(query)
	cache := optionCache{counts: c, cap: optionCap, byFold: make(map[string][]string)}
	var output strings.Builder
	last := 0
	changed := false
	for start := 0; start < len(tokens); {
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}
		end := start + 1
		for end < len(tokens) && tokens[end].adjacent {
			end++
		}
		selected, err := choose(ctx, tokens[start:end], c, &cache)
		if err != nil {
			return nil, false, err
		}
		for i, t := range tokens[start:end] {
			output.Write(query[last:t.start])
			original := string(query[t.start:t.end])
			replacement := original
			if t.ascii && selected[i] != t.score {
				replacement = applyCase(original, selected[i])
				changed = true
			}
			output.WriteString(replacement)
			last = t.end
		}
		start = end
	}
	output.Write(query[last:])
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	if !changed {
		return query, false, nil
	}
	return []byte(output.String()), true, nil
}

type optionCache struct {
	counts counts
	cap    int
	byFold map[string][]string
}

func (cache *optionCache) options(t token) []string {
	result := []string{t.score}
	if !t.ascii {
		return result
	}
	ordered, ok := cache.byFold[t.fold]
	if !ok {
		for form := range cache.counts.forms[t.fold] {
			ordered = append(ordered, form)
		}
		sort.Slice(ordered, func(i, j int) bool {
			a, b := ordered[i], ordered[j]
			if cache.counts.singles[a] != cache.counts.singles[b] {
				return cache.counts.singles[a] > cache.counts.singles[b]
			}
			return a < b
		})
		if len(ordered) > cache.cap {
			ordered = ordered[:cache.cap]
		}
		cache.byFold[t.fold] = ordered
	}
	for _, form := range ordered {
		if form != t.score && len(result) < cache.cap {
			result = append(result, form)
		}
	}
	return result
}

func applyCase(original, form string) string {
	var out strings.Builder
	i := 0
	for _, r := range form {
		if original[i] >= 'A' && original[i] <= 'Z' {
			r = unicode.ToUpper(r)
		}
		out.WriteRune(r)
		i++
	}
	return out.String()
}
