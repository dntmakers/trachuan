package retrieval

func filePreview(files []SourceFile, total, budget int, c cursor, empty bool) ([]SourceFile, int, bool, int) {
	total = max(total, len(files))
	if len(files) == 0 && !empty {
		return nil, 0, false, 0
	}
	cost := func(n int) int {
		fields := struct {
			Files   *[]SourceFile `json:"files,omitempty"`
			Omitted int           `json:"files_omitted,omitempty"`
		}{Omitted: total - n}
		if n > 0 || len(files) == 0 {
			prefix := files[:n]
			if prefix == nil {
				prefix = []SourceFile{}
			}
			fields.Files = &prefix
		}
		return c.size(fields) - 1
	}
	best := -1
	if cost(0) <= budget {
		best = 0
	}
	for n := 1; n <= len(files) && n <= 20; n++ {
		if readSize(files[:n]) > 8192 {
			break
		}
		if cost(n) <= budget {
			best = n
		}
	}
	if best < 0 {
		return nil, 0, false, 0
	}
	present := best > 0 || len(files) == 0
	prefix := files[:best]
	if present && prefix == nil {
		prefix = []SourceFile{}
	}
	return prefix, total - best, present, cost(best)
}
