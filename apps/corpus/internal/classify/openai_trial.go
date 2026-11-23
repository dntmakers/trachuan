package classify

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"danny.vn/trachuan/apps/corpus/internal/openaibatch"
)

const OpenAITrialMaxMicroUSD = 40_000_000

type OpenAITrialOptions struct {
	Country, Release, LabelsHash, Out string
	ExpectedFamiliesHash              string
	Recipe                            Recipe
	CapMicroUSD                       int64
	DryRun                            bool
	Progress                          io.Writer
}

type OpenAITrialSummary struct {
	Requests           int   `json:"requests"`
	CountedInputTokens int64 `json:"counted_input_tokens"`
	ReserveMicroUSD    int64 `json:"reserve_micro_usd"`
	SiblingMicroUSD    int64 `json:"sibling_micro_usd"`
}

type OpenAITrialClient struct {
	*openaibatch.Client
	credential []byte
}

func NewOpenAITrialClient(key, project string, transport http.RoundTripper) *OpenAITrialClient {
	return &OpenAITrialClient{openaibatch.NewClient(key, project, transport), []byte(key)}
}

type openAITrial struct {
	o      OpenAITrialOptions
	client *OpenAITrialClient
	state  openAITrialState
	counts map[string]int64
	input  []byte
	parts  []openAITrialItem
	result Submission
	nodes  []Node
}

type openAITrialItem struct {
	openaibatch.Item
	Index int
	Count int64
}

func RunOpenAIDocumentTrial(ctx context.Context, o OpenAITrialOptions, client *OpenAITrialClient, docs []Document, menu []Family) (OpenAITrialSummary, error) {
	return runOpenAITrial(ctx, "document", o, client, func(t *openAITrial) error {
		selection := DocumentSelection{LabelsHash: o.LabelsHash}
		for _, doc := range docs {
			selection.DocumentKeys = append(selection.DocumentKeys, doc.Key)
		}
		if err := selection.validate(); err != nil {
			return err
		}
		t.result.Families = menu
		for i, doc := range docs {
			body, countBody, err := BuildOpenAIDocumentRequest(o.Recipe, doc, menu)
			if err != nil {
				return err
			}
			n, err := t.count(ctx, body, countBody)
			if err != nil {
				return err
			}
			item := Item{Key: doc.Key, RequestHash: openAICountKey(body), InputTokens: n, Local: n > o.Recipe.InputBound}
			if i == 0 {
				item.Selection = &selection
			}
			t.result.Items = append(t.result.Items, item)
			t.result.Results[doc.Key] = Validate("", "", o.Recipe.F, menu)
			if !item.Local {
				t.parts = append(t.parts, openAITrialItem{openaibatch.Item{ID: strconv.Itoa(i), Body: body}, i, n})
			}
		}
		return nil
	})
}

func runOpenAITrial(ctx context.Context, stage string, o OpenAITrialOptions, client *OpenAITrialClient, plan func(*openAITrial) error) (OpenAITrialSummary, error) {
	var summary OpenAITrialSummary
	if o.Out == "" || o.Country == "" || o.Release == "" || !validKey(o.LabelsHash) || o.CapMicroUSD < 1 || o.CapMicroUSD > OpenAITrialMaxMicroUSD || client == nil || client.Client == nil || len(client.credential) == 0 || (o.Recipe.Article != nil) != (stage == "article") {
		return summary, fmt.Errorf("invalid OpenAI trial options or cap (maximum 40 USD)")
	}
	if _, err := openAIRecipe(o.Recipe); err != nil {
		return summary, err
	}
	t := openAITrial{o: o, client: client, result: Submission{Country: o.Country, Release: o.Release, Recipe: o.Recipe, Results: map[string]Outcome{}}}
	unlock, err := t.open(stage)
	if err != nil {
		return summary, err
	}
	defer unlock()
	if t.state.Phase != trialPreparing {
		return t.resume(ctx)
	}
	if err = plan(&t); err != nil {
		return summary, err
	}
	if summary, err = t.prepare(); err != nil || o.DryRun {
		return summary, err
	}
	return summary, t.execute(ctx)
}

func (t *openAITrial) execute(ctx context.Context) error {
	if len(t.parts) == 0 {
		return t.finish(openAITrialCost{Items: []openAITrialCostItem{}, Unpriced: []string{}, Complete: true})
	}
	return t.runBatch(ctx)
}

func (t *openAITrial) prepare() (OpenAITrialSummary, error) {
	summary := OpenAITrialSummary{Requests: len(t.parts)}
	items := make([]openaibatch.Item, 0, len(t.parts))
	for _, part := range t.parts {
		reserve, err := t.o.Recipe.reserve(t.state.PriceVersion, part.Count)
		if err != nil {
			return summary, err
		}
		summary.CountedInputTokens += part.Count
		summary.ReserveMicroUSD += reserve
		items = append(items, part.Item)
	}
	if len(items) != 0 {
		var err error
		t.input, err = openaibatch.Encode(items)
		if err != nil {
			return summary, err
		}
	}
	if err := t.admit(&summary); err != nil {
		return summary, err
	}
	return summary, nil
}
