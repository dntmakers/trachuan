package classify

import (
	"encoding/json"
	"reflect"
)

func retainedRequired(typ reflect.Type) []string {
	switch typ {
	case reflect.TypeOf(Submission{}):
		return []string{"id", "run_id", "country", "release", "screen_hash", "recipe", "items", "items_hash", "state", "provider_job_id", "created_at", "budget", "budget_micro_usd", "reserved_micro_usd", "settled", "cost_micro_usd"}
	case reflect.TypeOf(Recipe{}):
		return []string{"version", "model", "effort", "max_tokens", "input_bound", "families", "prompt_version", "schema_version", "taxonomy_hash"}
	case reflect.TypeOf(ArticleRecipe{}):
		return []string{"source_recipe_hash", "families_sha256", "prompt_version", "schema_version", "max_tokens"}
	case reflect.TypeOf(Item{}):
		return []string{"key", "request_hash", "counted_input_tokens"}
	case reflect.TypeOf(Family{}):
		return []string{"code", "name", "level_1_name"}
	case reflect.TypeOf(ArticleRequest{}):
		return []string{"document_key", "articles", "allowed_subtrees", "counted_input_tokens"}
	case reflect.TypeOf(Article{}):
		return []string{"anchor", "heading", "text"}
	case reflect.TypeOf(Node{}):
		return []string{"code", "name", "parent_code", "level", "active", "origin"}
	case reflect.TypeOf(DocumentSelection{}):
		return []string{"labels_sha256", "document_keys"}
	case reflect.TypeOf(FamilyArtifact{}):
		return []string{"version", "recipe_hash", "taxonomy_hash", "recipe", "documents"}
	case reflect.TypeOf(ArticleArtifact{}):
		return []string{"version", "country", "release", "recipe", "recipe_hash", "taxonomy_hash", "families_sha256", "labels_sha256", "articles", "dropped_anchors"}
	case reflect.TypeOf(ArticleMetadata{}):
		return []string{"labels_sha256"}
	case reflect.TypeOf(ArticleRecord{}):
		return []string{"document_key", "anchor", "result"}
	case reflect.TypeOf(Outcome{}), reflect.TypeOf(ArticleOutcome{}):
		return []string{"outcome"}
	case reflect.TypeOf(CodeConfidence{}):
		return []string{"code", "confidence"}
	case reflect.TypeOf(captureStart{}):
		return []string{"intent_id", "capture_id"}
	case reflect.TypeOf(captureEnd{}):
		return []string{"records"}
	case reflect.TypeOf(capturedResult{}):
		return []string{"custom_id", "type", "result"}
	case reflect.TypeOf(creatorRecord{}):
		return []string{"id", "items_hash", "at", "dispatched"}
	case reflect.TypeOf(batchRecord{}):
		return []string{"provider_job_id", "submitted_at", "deadline_at"}
	case reflect.TypeOf(abandonmentRecord{}):
		return []string{"id", "items_hash", "abandoned_at"}
	case reflect.TypeOf(settlementRecord{}):
		return []string{"id", "provider_job_id", "state", "cost_micro_usd", "price_version", "evidence_kind"}
	case reflect.TypeOf(attemptRecord{}):
		return []string{"submission_id", "provider_job_id", "item_index", "key", "outcome", "provider_body", "usage", "price_version"}
	case reflect.TypeOf(tokenUsage{}):
		return []string{"input_tokens", "cache_write_5m_tokens", "cache_read_tokens", "output_tokens"}
	case reflect.TypeOf(countedRequest{}):
		return []string{"key", "input_tokens"}
	case reflect.TypeOf(providerResponse{}):
		return []string{"intent_id", "capture_id", "operation", "account_fingerprint", "http_status", "body"}
	case reflect.TypeOf(providerRequestEvidence{}):
		return []string{"custom_id", "request_hash"}
	}
	return nil
}

func retainedConditionalRequired(typ reflect.Type, fields map[string]json.RawMessage) ([]string, error) {
	switch typ {
	case reflect.TypeOf(providerResponse{}):
		op, err := evidenceString(fields["operation"])
		if err != nil {
			return nil, err
		}
		switch op {
		case "create":
			return []string{"requests"}, nil
		case "get":
			return []string{"provider_job_id"}, nil
		}
	case reflect.TypeOf(captureStart{}):
		if value := fields["provider_job_id"]; value != nil {
			id, err := evidenceString(value)
			if err != nil {
				return nil, err
			}
			if id != "" {
				return nil, nil
			}
		}
		return []string{"after", "before"}, nil
	case reflect.TypeOf(settlementRecord{}):
		if value := fields["evidence_kind"]; value != nil {
			kind, err := evidenceString(value)
			if err != nil {
				return nil, err
			}
			if kind == "provider_results" {
				return []string{"capture_path", "capture_sha256"}, nil
			}
		}
	case reflect.TypeOf(abandonmentRecord{}):
		if fields["capture_path"] != nil || fields["capture_sha256"] != nil {
			return []string{"capture_path", "capture_sha256"}, nil
		}
	}
	return nil, nil
}
