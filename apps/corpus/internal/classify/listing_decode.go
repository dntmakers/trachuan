package classify

import (
	"encoding/json"
	"fmt"

	"danny.vn/trachuan/apps/corpus/internal/batchapi"
)

func decodeListingPage(raw string) (listingPage, error) {
	var page listingPage
	fields, err := batchapi.DecodeEvidenceObject([]byte(raw), "data", "has_more", "first_id", "last_id")
	if err != nil {
		return page, err
	}
	var entries []json.RawMessage
	if err = json.Unmarshal(fields["data"], &entries); err != nil || entries == nil {
		return page, fmt.Errorf("invalid listing data")
	}
	switch string(fields["has_more"]) {
	case "true":
		v := true
		page.HasMore = &v
	case "false":
		v := false
		page.HasMore = &v
	default:
		return page, fmt.Errorf("invalid listing continuation")
	}
	for _, key := range []string{"first_id", "last_id"} {
		value := fields[key]
		if len(value) == 0 || string(value) == "null" {
			continue
		}
		v, fieldErr := evidenceString(value)
		if fieldErr != nil {
			return page, fieldErr
		}
		if key == "first_id" {
			page.FirstID = &v
		} else {
			page.LastID = &v
		}
	}
	page.Data = make([]listingBatch, 0, len(entries))
	for _, rawBatch := range entries {
		batch, batchErr := decodeListingBatch(rawBatch)
		if batchErr != nil {
			return page, batchErr
		}
		page.Data = append(page.Data, batch)
	}
	return page, nil
}

func decodeListingBatch(raw []byte) (listingBatch, error) {
	var batch listingBatch
	fields, err := batchapi.DecodeEvidenceObject(raw, "id", "created_at", "processing_status", "request_counts")
	if err != nil {
		return batch, err
	}
	if batch.ID, err = evidenceString(fields["id"]); err != nil {
		return batch, err
	}
	if raw := fields["created_at"]; len(raw) == 0 || json.Unmarshal(raw, &batch.CreatedAt) != nil || batch.CreatedAt.IsZero() {
		return batch, fmt.Errorf("invalid batch creation time")
	}
	if batch.Status, err = evidenceString(fields["processing_status"]); err != nil || batch.Status == "" {
		return batch, fmt.Errorf("invalid batch processing status")
	}
	if batch.Counts, err = decodeListingCounts(fields["request_counts"]); err != nil {
		return batch, err
	}
	if batch.Counts.Processing == nil || batch.Counts.Succeeded == nil || batch.Counts.Errored == nil || batch.Counts.Canceled == nil || batch.Counts.Expired == nil {
		return batch, fmt.Errorf("missing batch request count")
	}
	return batch, nil
}

func evidenceString(raw []byte) (string, error) {
	var value string
	if len(raw) == 0 || raw[0] != '"' || json.Unmarshal(raw, &value) != nil {
		return "", fmt.Errorf("invalid evidence string")
	}
	return value, nil
}

func decodeListingCounts(raw []byte) (*listingCounts, error) {
	fields, err := batchapi.DecodeEvidenceObject(raw, "processing", "succeeded", "errored", "canceled", "expired")
	if err != nil {
		return nil, err
	}
	counts := &listingCounts{}
	for _, field := range []struct {
		name string
		dst  **int64
	}{
		{"processing", &counts.Processing}, {"succeeded", &counts.Succeeded}, {"errored", &counts.Errored}, {"canceled", &counts.Canceled}, {"expired", &counts.Expired},
	} {
		if fields[field.name] == nil {
			continue
		}
		n, ok := batchapi.TokenInteger(fields[field.name], 0)
		if !ok {
			return nil, fmt.Errorf("invalid batch request count")
		}
		*field.dst = &n
	}
	return counts, nil
}
