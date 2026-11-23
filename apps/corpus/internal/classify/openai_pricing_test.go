package classify

import (
	"strings"
	"testing"
)

func TestOpenAIReserve(t *testing.T) {
	for _, tt := range []struct {
		model string
		want  int64
	}{{"gpt-6.2-sol", 7}, {"gpt-6.2-astra", 25}} {
		got, err := openAIReserve(tt.model, openAIBatchPriceVersion, 1, 1, 1)
		if err != nil || got != tt.want {
			t.Errorf("%s: got %d, %v", tt.model, got, err)
		}
	}
	for _, n := range []int64{0, 272001} {
		if _, err := openAIReserve("gpt-6.2-sol", openAIBatchPriceVersion, n, 272001, 1); err == nil {
			t.Errorf("accepted %d", n)
		}
	}
	if _, ok := priceForModel("gpt-6.2-sol"); ok {
		t.Fatal("GPT accepted by Anthropic pricing")
	}
}

func TestOpenAIUsage(t *testing.T) {
	raw := `{"model":"gpt-6.2-sol","usage":{"input_tokens":10,"input_tokens_details":{"cached_tokens":2,"cache_write_tokens":4,"cache_write_tokens_details":{"ttl_30m_tokens":4,"ttl_12h_tokens":0}},"output_tokens":3,"total_tokens":13,"output_tokens_details":{"reasoning_tokens":2}}}`
	got, err := openAIPriceUsage("gpt-6.2-sol", openAIBatchPriceVersion, 10, 3, []byte(raw), nil)
	if err != nil || got != 25 {
		t.Fatalf("got %d, %v", got, err)
	}
	for _, bad := range []string{
		strings.Replace(raw, `"cached_tokens":2`, `"Cached_tokens":2`, 1),
		strings.Replace(raw, `"ttl_12h_tokens":0`, `"ttl_12h_tokens":1`, 1),
		strings.Replace(raw, `"reasoning_tokens":2`, `"reasoning_tokens":4`, 1),
		strings.Replace(raw, `"gpt-6.2-sol"`, `"gpt-6.2-astra"`, 1),
		strings.Replace(raw, `"cached_tokens":2`, `"cached_tokens":2,"cached_tokens":2`, 1),
	} {
		if _, err := openAIPriceUsage("gpt-6.2-sol", openAIBatchPriceVersion, 10, 3, []byte(bad), nil); err == nil {
			t.Errorf("accepted %s", bad)
		}
	}
}

func TestOpenAIMissingTTLRequiresBothOptions(t *testing.T) {
	response := `{"model":"gpt-6.2-astra","prompt_cache_options":{"mode":"explicit","ttl":"30m"},"usage":{"input_tokens":2,"input_tokens_details":{"cached_tokens":0,"cache_write_tokens":1},"output_tokens":0,"total_tokens":2,"output_tokens_details":{"reasoning_tokens":0}}}`
	request := `{"model":"gpt-6.2-astra","prompt_cache_options":{"mode":"explicit","ttl":"30m"},"input":[{"role":"developer","content":[{"type":"input_text","text":"x","prompt_cache_breakpoint":{"mode":"explicit"}}]}]}`
	got, err := openAIPriceUsage("gpt-6.2-astra", openAIBatchPriceVersion, 2, 1, []byte(response), []byte(request))
	if err != nil || got != 9 {
		t.Fatalf("got %d, %v", got, err)
	}
	for _, bad := range []string{strings.Replace(request, `"ttl":"30m"`, `"ttl":"12h"`, 1), strings.Replace(request, `"mode":"explicit"}`, `"mode":"explicit","ttl":"12h"}`, 1), strings.Replace(request, `gpt-6.2-astra`, `gpt-6.2-sol`, 1)} {
		if _, err := openAIPriceUsage("gpt-6.2-astra", openAIBatchPriceVersion, 2, 1, []byte(response), []byte(bad)); err == nil {
			t.Errorf("accepted %s", bad)
		}
	}
}

func TestOpenAITotalsAndTier(t *testing.T) {
	raw := `{"model":"gpt-6.2-sol","service_tier":"default","usage":{"input_tokens":1,"input_tokens_details":{"cached_tokens":0,"cache_write_tokens":0},"output_tokens":1,"total_tokens":2,"output_tokens_details":{"reasoning_tokens":0}}}`
	for _, bad := range []string{
		strings.Replace(raw, `,"total_tokens":2`, ``, 1),
		strings.Replace(raw, `"total_tokens":2`, `"Total_tokens":2`, 1),
		strings.Replace(raw, `"total_tokens":2`, `"total_tokens":3`, 1),
		strings.Replace(raw, `"total_tokens":2`, `"total_tokens":2.0`, 1),
		strings.Replace(raw, `"default"`, `"priority"`, 1),
		strings.Replace(raw, `"service_tier"`, `"Service_tier"`, 1),
	} {
		if _, err := openAIPriceUsage("gpt-6.2-sol", openAIBatchPriceVersion, 1, 1, []byte(bad), nil); err == nil {
			t.Errorf("accepted %s", bad)
		}
	}
	for _, tier := range []string{`"default"`, `null`} {
		valid := strings.Replace(raw, `"default"`, tier, 1)
		if _, err := openAIPriceUsage("gpt-6.2-sol", openAIBatchPriceVersion, 1, 1, []byte(valid), nil); err != nil {
			t.Errorf("tier %s: %v", tier, err)
		}
	}
}

func TestOpenAIRequestBreakpointPlacement(t *testing.T) {
	response := `{"model":"gpt-6.2-sol","prompt_cache_options":{"mode":"explicit","ttl":"30m"},"usage":{"input_tokens":2,"input_tokens_details":{"cached_tokens":0,"cache_write_tokens":1},"output_tokens":0,"total_tokens":2,"output_tokens_details":{"reasoning_tokens":0}}}`
	prefix := `{"model":"gpt-6.2-sol","prompt_cache_options":{"mode":"explicit","ttl":"30m"},"input":`
	breakpoint := `{"type":"input_text","text":"x","prompt_cache_breakpoint":{"mode":"explicit"}}`
	for _, input := range []string{
		`[{"role":"user","content":[` + breakpoint + `]}]`,
		`[{"role":"developer","prompt_cache_breakpoint":{"mode":"explicit"},"content":[{"type":"input_text","text":"x"}]}]`,
		`[{"role":"developer","content":[` + breakpoint + `,{"type":"input_text","text":"later"}]}]`,
		`[{"role":"developer","content":[` + breakpoint + `]},{"role":"developer","content":[{"type":"input_text","text":"later"}]}]`,
		`[{"role":"developer","content":[` + breakpoint + `,` + breakpoint + `]}]`,
	} {
		if _, err := openAIPriceUsage("gpt-6.2-sol", openAIBatchPriceVersion, 2, 1, []byte(response), []byte(prefix+input+`}`)); err == nil {
			t.Errorf("accepted %s", input)
		}
	}
}

func TestOpenAIReservationBoundaries(t *testing.T) {
	for _, tt := range []struct {
		input, bound int64
		want         int64
		valid        bool
	}{{0, 272000, 0, false}, {272001, 272000, 0, false}, {1, 0, 0, false}, {1, 272001, 0, false}, {272000, 272000, 340005, true}} {
		got, err := openAIReserve("gpt-6.2-sol", openAIBatchPriceVersion, tt.input, tt.bound, 1)
		if (err == nil) != tt.valid || tt.valid && got != tt.want {
			t.Errorf("input=%d bound=%d got %d %v", tt.input, tt.bound, got, err)
		}
	}
}
