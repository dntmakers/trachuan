package classify

import (
	"fmt"
	"math"
)

const (
	batchPriceVersion = "anthropic-batch-2026-11-22"
	pilotPriceVersion = "anthropic-batch-pilot"
	BatchPriceSource  = "https://platform.claude.com/docs/en/about-claude/pricing"
	priceUnit         = 2000
)

type tokenUsage struct {
	Input      int64 `json:"input_tokens"`
	CacheWrite int64 `json:"cache_write_5m_tokens"`
	CacheRead  int64 `json:"cache_read_tokens"`
	Output     int64 `json:"output_tokens"`
}

type batchPrice struct{ input, write, read, output int64 }

type openAIPriceRow struct{ input, read, write30m, write12h, output int64 }

const (
	openAIBatchPriceVersion = "openai-batch-2026-11-22"
	openAIBatchPriceSource  = "https://developers.openai.com/api/docs/pricing"
)

func openAIPriceForModel(model string) (openAIPriceRow, bool) {
	switch model {
	case "gpt-6.2-sol":
		return openAIPriceRow{1000, 50, 1250, 2500, 5000}, true
	case "gpt-6.2-astra":
		return openAIPriceRow{4000, 200, 5000, 10000, 20000}, true
	default:
		return openAIPriceRow{}, false
	}
}

func priceForModel(model string) (batchPrice, bool) {
	switch model {
	case "claude-haiku-5-5":
		return batchPrice{100, 125, 10, 500}, true
	case "claude-sonnet-5-5":
		return batchPrice{2000, 2500, 100, 10000}, true
	case "claude-opus-5-5":
		return batchPrice{4000, 5000, 200, 20000}, true
	case "claude-fable-5-2":
		return batchPrice{8000, 10000, 200, 40000}, true
	default:
		return batchPrice{}, false
	}
}

func (r Recipe) Reserve(input int64) (int64, error) {
	if err := r.Validate(); err != nil {
		return 0, err
	}
	return r.reserve(batchPriceVersion, input)
}

func (r Recipe) reserve(version string, input int64) (int64, error) {
	if input < 1 || input > r.InputBound {
		return 0, fmt.Errorf("counted input exceeds classification recipe bound or is empty")
	}
	if version == "" || version == pilotPriceVersion {
		return r.cost(pilotPriceVersion, tokenUsage{Input: input, Output: r.outputTokens()})
	}
	p, ok := priceForModel(r.Model)
	if !ok {
		return 0, fmt.Errorf("unknown classification model")
	}
	u := tokenUsage{Output: r.outputTokens()}
	switch max(p.input, p.write, p.read) {
	case p.write:
		u.CacheWrite = input
	case p.read:
		u.CacheRead = input
	default:
		u.Input = input
	}
	return r.cost(version, u)
}

func (r Recipe) cost(version string, u tokenUsage) (int64, error) {
	price, ok := priceForModel(r.Model)
	if !ok || (version != batchPriceVersion && version != pilotPriceVersion) {
		return 0, fmt.Errorf("unknown classification model or price table")
	}
	if version == pilotPriceVersion && (u.CacheRead != 0 || u.CacheWrite != 0) {
		return 0, fmt.Errorf("pilot price table does not cover cache usage")
	}
	if u.Input < 0 || u.CacheWrite < 0 || u.CacheRead < 0 || u.Output < 0 || u.Output > r.outputTokens() ||
		(r.Model == "claude-haiku-5-5" && (u.Input > 100000 || u.CacheWrite > 100000-u.Input || u.CacheRead > 100000-u.Input-u.CacheWrite)) {
		return 0, fmt.Errorf("classification usage outside verified pricing")
	}
	var total int64
	for _, pair := range [][2]int64{{price.input, u.Input}, {price.write, u.CacheWrite}, {price.read, u.CacheRead}, {price.output, u.Output}} {
		if pair[1] > (math.MaxInt64-total-priceUnit+1)/pair[0] {
			return 0, fmt.Errorf("classification usage cost overflows")
		}
		total += pair[0] * pair[1]
	}
	return (total + priceUnit - 1) / priceUnit, nil
}

func (s Submission) priceVersion() string {
	if s.PriceVersion == "" {
		return pilotPriceVersion
	}
	return s.PriceVersion
}
