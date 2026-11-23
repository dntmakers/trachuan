package classify

import (
	"encoding/json"

	"danny.vn/trachuan/apps/corpus/internal/batchapi"
)

type openAITrialCostItem struct {
	CustomID           string          `json:"custom_id"`
	CountedInputTokens int64           `json:"counted_input_tokens"`
	Usage              json.RawMessage `json:"usage,omitempty"`
	UsageInputTokens   *int64          `json:"usage_input_tokens,omitempty"`
	InputTokenDelta    *int64          `json:"input_token_delta,omitempty"`
	CostMicroUSD       *int64          `json:"cost_micro_usd"`
	Status             string          `json:"status"`
}

type openAITrialCost struct {
	Items              []openAITrialCostItem `json:"items"`
	Unpriced           []string              `json:"unpriced"`
	CountedInputTokens int64                 `json:"counted_input_tokens"`
	PricedInputTokens  int64                 `json:"priced_input_tokens"`
	PricedOutputTokens int64                 `json:"priced_output_tokens"`
	PricedMicroUSD     int64                 `json:"priced_micro_usd"`
	Complete           bool                  `json:"complete"`
	Issue              string                `json:"issue,omitempty"`
}

type openAITrialUsage struct{ input, output, cached, reasoning int64 }

func (t *openAITrial) price(lines []openAICoveredResult, covered bool, batch openAIBatchTerminal) openAITrialCost {
	cost := openAITrialCost{Items: make([]openAITrialCostItem, 0, len(t.parts)), Unpriced: []string{}}
	byID := make(map[string]openAIResultLine, len(lines))
	for _, line := range lines {
		byID[line.CustomID] = line.Result
	}
	var totals openAITrialUsage
	for _, part := range t.parts {
		line, ok := byID[part.ID]
		item, usage := t.priceItem(part, line, ok)
		cost.Items = append(cost.Items, item)
		cost.CountedInputTokens += part.Count
		if item.CostMicroUSD == nil {
			cost.Unpriced = append(cost.Unpriced, part.ID)
			continue
		}
		cost.PricedMicroUSD += *item.CostMicroUSD
		totals.input += usage.input
		totals.output += usage.output
		totals.cached += usage.cached
		totals.reasoning += usage.reasoning
		t.apply(part.Index, line)
	}
	cost.PricedInputTokens, cost.PricedOutputTokens = totals.input, totals.output
	switch {
	case !covered:
		cost.Issue = "batch coverage incomplete or cancelled"
	case len(cost.Unpriced) > 0:
		cost.Issue = "unpriced results"
	case batch.UsageState == openAIAggregatePresent && !totals.matches(batch.Usage):
		cost.Issue = "batch aggregate contradicts item usage"
	case cost.PricedMicroUSD > t.state.ReserveMicroUSD:
		cost.Issue = "observed cost exceeds reserve"
	default:
		cost.Complete = true
	}
	return cost
}

func (t *openAITrial) priceItem(part openAITrialItem, line openAIResultLine, found bool) (openAITrialCostItem, openAITrialUsage) {
	item := openAITrialCostItem{CustomID: part.ID, CountedInputTokens: part.Count, Status: "missing"}
	if !found {
		return item, openAITrialUsage{}
	}
	item.Status = string(line.Status)
	if line.ZeroCost {
		item.CostMicroUSD = new(int64)
		return item, openAITrialUsage{}
	}
	fields, _ := batchapi.DecodeEvidenceProjection(line.Body, "usage")
	item.Usage = fields["usage"]
	usage, _ := openAIObject(item.Usage, "input_tokens", "output_tokens", "input_tokens_details", "output_tokens_details")
	if input, ok := batchapi.TokenInteger(usage["input_tokens"], 0); ok {
		delta := input - part.Count
		item.UsageInputTokens, item.InputTokenDelta = &input, &delta
	}
	amount, err := openAIPriceUsage(t.state.Model, t.state.PriceVersion, t.o.Recipe.InputBound, t.o.Recipe.outputTokens(), line.Body, part.Body)
	if err != nil {
		return item, openAITrialUsage{}
	}
	item.CostMicroUSD = &amount
	return item, trialUsage(usage)
}

func trialUsage(fields map[string]json.RawMessage) openAITrialUsage {
	in, _ := openAICount(fields, "input_tokens")
	out, _ := openAICount(fields, "output_tokens")
	inDetails, _ := openAIObject(fields["input_tokens_details"], "cached_tokens")
	outDetails, _ := openAIObject(fields["output_tokens_details"], "reasoning_tokens")
	cached, _ := openAICount(inDetails, "cached_tokens")
	reasoning, _ := openAICount(outDetails, "reasoning_tokens")
	return openAITrialUsage{in, out, cached, reasoning}
}

func (u openAITrialUsage) matches(raw []byte) bool {
	fields, err := openAIObject(raw, "input_tokens", "output_tokens", "total_tokens", "input_tokens_details", "output_tokens_details")
	if err != nil {
		return false
	}
	a := trialUsage(fields)
	total, err := openAICount(fields, "total_tokens")
	return err == nil && total == u.input+u.output && a.input == u.input && a.output == u.output && (fields["input_tokens_details"] == nil || a.cached == u.cached) && (fields["output_tokens_details"] == nil || a.reasoning == u.reasoning)
}
