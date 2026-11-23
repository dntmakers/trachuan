package classify

import (
	"strings"
	"testing"
)

func TestOpenAITrialModels(t *testing.T) {
	for _, tt := range []struct {
		model string
		row   openAIPriceRow
	}{
		{"gpt-6.1-terra", openAIPriceRow{1000, 50, 1250, 2500, 5000}},
		{"gpt-6-luna", openAIPriceRow{100, 10, 125, 250, 500}},
	} {
		t.Run(tt.model, func(t *testing.T) {
			row, ok := openAIPriceForModel(tt.model)
			if !ok || row != tt.row {
				t.Fatalf("price = %+v, %v", row, ok)
			}
			r, err := NewOpenAIRecipe(tt.model, strings.Repeat("a", 64))
			if err != nil {
				t.Fatal(err)
			}
			for _, effort := range []string{"low", "medium", "high", "xhigh", "max"} {
				r.Effort = effort
				if _, err := openAIRecipe(r); err != nil {
					t.Errorf("%s: %v", effort, err)
				}
			}
			r.Effort = "none"
			if _, err := openAIRecipe(r); err == nil {
				t.Error("accepted none outside recipe")
			}
		})
	}
}

func TestOpenAITrialFractionalPrice(t *testing.T) {
	got, err := openAIReserve("gpt-6-luna", openAIBatchPriceVersion, 16000, 16000, 16)
	if err != nil || got != 1004 {
		t.Fatalf("reserve = %d, %v", got, err)
	}
	raw := `{"model":"gpt-6-luna","usage":{"input_tokens":20000,"input_tokens_details":{"cached_tokens":0,"cache_write_tokens":20000,"cache_write_tokens_details":{"ttl_30m_tokens":20000,"ttl_12h_tokens":0}},"output_tokens":0,"total_tokens":20000,"output_tokens_details":{"reasoning_tokens":0}}}`
	got, err = openAIPriceUsage("gpt-6-luna", openAIBatchPriceVersion, 20000, 16, []byte(raw), nil)
	if err != nil || got != 1250 {
		t.Fatalf("usage = %d, %v", got, err)
	}
}
