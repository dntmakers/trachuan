package evaluation

import "danny.vn/trachuan/internal/retrieval"

func annMeasure(id, cohort string, exact, raw, effective []retrieval.Candidate, fallback bool, depth int) ANNMeasure {
	exact = exact[:min(len(exact), depth)]
	return ANNMeasure{id, cohort, len(exact), overlap(exact, raw), overlap(exact, effective), fallback}
}

func overlap(exact, other []retrieval.Candidate) int {
	ids := map[int64]bool{}
	for _, c := range exact {
		ids[c.ChunkID] = true
	}
	n := 0
	for _, c := range other {
		if ids[c.ChunkID] {
			n++
		}
	}
	return n
}
