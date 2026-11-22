package classify

import "danny.vn/trachuan/apps/corpus/internal/batchapi"

func priceUsage(s Submission, raw string) (tokenUsage, *int64) {
	var usage tokenUsage
	fields, err := batchapi.DecodeEvidenceObject([]byte(raw), "input_tokens", "output_tokens", "cache_creation_input_tokens", "cache_read_input_tokens", "cache_creation")
	if err != nil {
		return usage, nil
	}
	for _, v := range []struct {
		field    string
		count    *int64
		optional bool
	}{
		{"input_tokens", &usage.Input, false},
		{"output_tokens", &usage.Output, false},
		{"cache_creation_input_tokens", &usage.CacheWrite, true},
		{"cache_read_input_tokens", &usage.CacheRead, true},
	} {
		if fields[v.field] == nil && v.optional {
			continue
		}
		count, ok := batchapi.TokenInteger(fields[v.field], 0)
		if !ok {
			return tokenUsage{}, nil
		}
		*v.count = count
	}
	if rawTTL := fields["cache_creation"]; rawTTL != nil {
		ttl, readErr := closedObject(rawTTL, []string{"ephemeral_1h_input_tokens", "ephemeral_5m_input_tokens"})
		hour, hourOK := batchapi.TokenInteger(ttl["ephemeral_1h_input_tokens"], 0)
		five, fiveOK := batchapi.TokenInteger(ttl["ephemeral_5m_input_tokens"], 0)
		if readErr != nil || !hourOK || !fiveOK || hour != 0 || five != usage.CacheWrite {
			return usage, nil
		}
	}
	cost, err := s.Recipe.cost(s.priceVersion(), usage)
	if err != nil {
		return usage, nil
	}
	return usage, &cost
}
