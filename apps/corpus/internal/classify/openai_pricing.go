package classify

import (
	"encoding/json"
	"fmt"
	"math"

	"danny.vn/trachuan/apps/corpus/internal/batchapi"
)

const openAIPriceUnit int64 = 2000

func openAIPrice(model, version string, input, bound, output int64) (openAIPriceRow, error) {
	p, ok := openAIPriceForModel(model)
	if !ok || version != openAIBatchPriceVersion || bound < 1 || bound > 272000 || input < 1 || input > bound || output < 1 || output > 32000 {
		return openAIPriceRow{}, fmt.Errorf("invalid OpenAI pricing identity or bounds")
	}
	return p, nil
}

func openAIReserve(model, version string, input, bound, output int64) (int64, error) {
	p, err := openAIPrice(model, version, input, bound, output)
	if err != nil {
		return 0, err
	}
	return openAICost([][2]int64{{p.write30m, input}, {p.output, output}})
}

func openAICost(pairs [][2]int64) (int64, error) {
	var total int64
	for _, pair := range pairs {
		if pair[0] <= 0 || pair[1] < 0 || pair[1] > (math.MaxInt64-total-(openAIPriceUnit-1))/pair[0] {
			return 0, fmt.Errorf("invalid OpenAI cost")
		}
		total += pair[0] * pair[1]
	}
	return (total + openAIPriceUnit - 1) / openAIPriceUnit, nil
}

func openAIObject(raw json.RawMessage, names ...string) (map[string]json.RawMessage, error) {
	return batchapi.DecodeEvidenceObject(raw, names...)
}

func openAICount(fields map[string]json.RawMessage, name string) (int64, error) {
	n, ok := batchapi.TokenInteger(fields[name], 0)
	if !ok {
		return 0, fmt.Errorf("invalid OpenAI usage evidence")
	}
	return n, nil
}

func openAIPriceUsage(model, version string, bound, maxOutput int64, response, request []byte) (int64, error) {
	p, err := openAIPrice(model, version, 1, bound, maxOutput)
	if err != nil {
		return 0, err
	}
	fail := func() (int64, error) { return 0, fmt.Errorf("invalid OpenAI usage evidence") }
	root, err := openAIObject(response, "model", "usage", "prompt_cache_options", "service_tier")
	if err != nil {
		return fail()
	}
	var actual string
	if json.Unmarshal(root["model"], &actual) != nil || actual != model {
		return fail()
	}
	if tier, exists := root["service_tier"]; exists && string(tier) != "null" {
		var value string
		if json.Unmarshal(tier, &value) != nil || value != "default" {
			return fail()
		}
	}
	usage, err := openAIObject(root["usage"], "input_tokens", "input_tokens_details", "output_tokens", "output_tokens_details", "total_tokens")
	if err != nil {
		return fail()
	}
	input, e1 := openAICount(usage, "input_tokens")
	output, e2 := openAICount(usage, "output_tokens")
	total, e3 := openAICount(usage, "total_tokens")
	if e1 != nil || e2 != nil || e3 != nil || input < 1 || input > bound || output > maxOutput || total != input+output {
		return fail()
	}
	details, err := openAIObject(usage["input_tokens_details"], "cached_tokens", "cache_write_tokens", "cache_write_tokens_details")
	if err != nil {
		return fail()
	}
	read, e1 := openAICount(details, "cached_tokens")
	write, e2 := openAICount(details, "cache_write_tokens")
	if e1 != nil || e2 != nil || read > input || write > input-read {
		return fail()
	}
	outDetails, err := openAIObject(usage["output_tokens_details"], "reasoning_tokens")
	if err != nil {
		return fail()
	}
	reasoning, err := openAICount(outDetails, "reasoning_tokens")
	if err != nil || reasoning > output {
		return fail()
	}
	if ttl, exists := details["cache_write_tokens_details"]; exists {
		split, err := openAIObject(ttl, "ttl_30m_tokens", "ttl_12h_tokens")
		if err != nil {
			return fail()
		}
		short, e1 := openAICount(split, "ttl_30m_tokens")
		long, e2 := openAICount(split, "ttl_12h_tokens")
		if e1 != nil || e2 != nil || long != 0 || short != write {
			return fail()
		}
	} else if write != 0 && !openAI30mEvidence(model, request, root["prompt_cache_options"]) {
		return fail()
	}
	return openAICost([][2]int64{{p.input, input - read - write}, {p.read, read}, {p.write30m, write}, {p.output, output}})
}

func openAI30mEvidence(model string, request, returned json.RawMessage) bool {
	root, err := openAIObject(request, "model", "prompt_cache_options", "input", "text", "reasoning", "truncation", "store", "max_output_tokens")
	var requestModel string
	if err != nil || !openAIAllowed(root, "model", "prompt_cache_options", "input", "text", "reasoning", "truncation", "store", "max_output_tokens") || json.Unmarshal(root["model"], &requestModel) != nil || requestModel != model || !openAIOptions(root["prompt_cache_options"]) || !openAIOptions(returned) {
		return false
	}
	return openAIStableBreakpoint(root["input"])
}

func openAIOptions(raw json.RawMessage) bool {
	fields, err := openAIObject(raw, "mode", "ttl")
	if err != nil {
		return false
	}
	var mode, ttl string
	return json.Unmarshal(fields["mode"], &mode) == nil && mode == "explicit" && json.Unmarshal(fields["ttl"], &ttl) == nil && ttl == "30m"
}

func openAIAllowed(fields map[string]json.RawMessage, names ...string) bool {
	for key := range fields {
		found := false
		for _, name := range names {
			if key == name {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func openAIStableBreakpoint(raw json.RawMessage) bool {
	var messages []json.RawMessage
	if json.Unmarshal(raw, &messages) != nil || len(messages) < 1 || len(messages) > 2 {
		return false
	}
	for i, rawMessage := range messages {
		message, err := openAIObject(rawMessage, "role", "type", "content")
		if err != nil || !openAIAllowed(message, "role", "type", "content") {
			return false
		}
		var role string
		if json.Unmarshal(message["role"], &role) != nil || (i == 0 && role != "developer") || (i == 1 && role != "user") {
			return false
		}
		if rawType, ok := message["type"]; ok {
			var typ string
			if json.Unmarshal(rawType, &typ) != nil || typ != "message" {
				return false
			}
		}
		var blocks []json.RawMessage
		if json.Unmarshal(message["content"], &blocks) != nil || len(blocks) == 0 {
			return false
		}
		for j, rawBlock := range blocks {
			block, err := openAIObject(rawBlock, "type", "text", "prompt_cache_breakpoint")
			if err != nil || !openAIAllowed(block, "type", "text", "prompt_cache_breakpoint") {
				return false
			}
			var typ, text string
			if json.Unmarshal(block["type"], &typ) != nil || typ != "input_text" || json.Unmarshal(block["text"], &text) != nil {
				return false
			}
			bp, exists := block["prompt_cache_breakpoint"]
			required := i == 0 && j == len(blocks)-1
			if exists != required {
				return false
			}
			if exists && !openAIBreakpoint(bp) {
				return false
			}
		}
	}
	return true
}

func openAIBreakpoint(raw json.RawMessage) bool {
	fields, err := openAIObject(raw, "mode", "ttl")
	if err != nil || !openAIAllowed(fields, "mode", "ttl") {
		return false
	}
	var mode string
	if json.Unmarshal(fields["mode"], &mode) != nil || mode != "explicit" {
		return false
	}
	if ttl, ok := fields["ttl"]; ok {
		var value string
		if json.Unmarshal(ttl, &value) != nil || value != "30m" {
			return false
		}
	}
	return true
}
