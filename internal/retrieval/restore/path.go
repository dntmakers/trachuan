package restore

import (
	"context"
	"sort"
)

type state struct {
	form                           string
	pairs, singles, previous, rank int
}

func choose(ctx context.Context, tokens []token, c counts, cache *optionCache) ([]string, error) {
	layers := make([][]state, 0, len(tokens))
	for _, t := range tokens {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		forms := cache.options(t)
		layer := make([]state, len(forms))
		for i, form := range forms {
			s := state{form: form, singles: c.singles[form], previous: -1}
			if len(layers) > 0 {
				prev := layers[len(layers)-1]
				for j, p := range prev {
					pairSum := p.pairs + c.pairs[pair{p.form, form}]
					singleSum := p.singles + c.singles[form]
					if s.previous < 0 || pairSum > s.pairs || (pairSum == s.pairs && (singleSum > s.singles || (singleSum == s.singles && p.rank < prev[s.previous].rank))) {
						s.pairs = pairSum
						s.singles = singleSum
						s.previous = j
					}
				}
			}
			layer[i] = s
		}
		order := make([]int, len(layer))
		for i := range order {
			order[i] = i
		}
		sort.Slice(order, func(i, j int) bool {
			a, b := layer[order[i]], layer[order[j]]
			if len(layers) > 0 {
				prev := layers[len(layers)-1]
				if prev[a.previous].rank != prev[b.previous].rank {
					return prev[a.previous].rank < prev[b.previous].rank
				}
			}
			return a.form < b.form
		})
		for rank, i := range order {
			layer[i].rank = rank
		}
		layers = append(layers, layer)
	}
	last := layers[len(layers)-1]
	best := 0
	for i := 1; i < len(last); i++ {
		a, b := last[i], last[best]
		if a.pairs > b.pairs || (a.pairs == b.pairs && (a.singles > b.singles || (a.singles == b.singles && a.rank < b.rank))) {
			best = i
		}
	}
	selected := make([]string, len(layers))
	for i := len(layers) - 1; i >= 0; i-- {
		s := layers[i][best]
		selected[i] = s.form
		best = s.previous
	}
	return selected, nil
}
