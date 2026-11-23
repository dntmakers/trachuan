package classify

import (
	"encoding/json"
	"fmt"

	"danny.vn/trachuan/apps/corpus/internal/batchapi"
)

type openAIListingPair struct{ SubmissionID, InputFileID string }
type openAIListingCapture struct {
	Project, Fingerprint, After string
	Raw                         []byte
}
type openAIListingCandidate struct {
	BatchID string
	Pair    openAIListingPair
}
type openAIListingDiscovery struct {
	TargetFound bool
	Candidates  []openAIListingCandidate
}

func verifyOpenAIListing(pages []openAIListingCapture, project, fingerprint string, start, end int64, target openAIListingPair, pairs []openAIListingPair) (openAIListingDiscovery, error) {
	fail := func() (openAIListingDiscovery, error) {
		return openAIListingDiscovery{}, fmt.Errorf("invalid OpenAI listing evidence")
	}
	validPair := func(p openAIListingPair) bool { return openAIUsableID(p.SubmissionID) && openAIUsableID(p.InputFileID) }
	if !openAIProjectValid(project) || !validID(fingerprint) || start <= 0 || end < start || !validPair(target) || len(pages) == 0 {
		return fail()
	}
	targetPairs := 0
	for _, p := range pairs {
		if !validPair(p) {
			return fail()
		}
		if p == target {
			targetPairs++
		}
	}
	if targetPairs != 1 {
		return fail()
	}
	var result openAIListingDiscovery
	seen := map[string]bool{}
	seenPairs := map[openAIListingPair]bool{}
	after := ""
	for i, capture := range pages {
		if capture.Project != project || capture.Fingerprint != fingerprint || capture.After != after {
			return fail()
		}
		fields, err := batchapi.DecodeEvidenceProjection(capture.Raw, "object", "data", "has_more", "first_id", "last_id")
		if err != nil {
			return fail()
		}
		object, err := evidenceString(fields["object"])
		if err != nil || object != "list" {
			return fail()
		}
		var data []json.RawMessage
		if json.Unmarshal(fields["data"], &data) != nil || data == nil {
			return fail()
		}
		more := false
		switch string(fields["has_more"]) {
		case "true":
			more = true
		case "false":
		default:
			return fail()
		}
		if more != (i < len(pages)-1) || more && len(data) == 0 {
			return fail()
		}
		first, last := "", ""
		for _, raw := range data {
			f, e := batchapi.DecodeEvidenceProjection(raw, "object", "id", "created_at")
			if e != nil {
				return fail()
			}
			object, e := evidenceString(f["object"])
			if e != nil || object != "batch" {
				return fail()
			}
			id, e := openAIEvidenceID(f["id"])
			timestamp, ok := batchapi.TokenInteger(f["created_at"], 1)
			if e != nil || !openAIUsableID(id) || !ok || seen[id] {
				return fail()
			}
			seen[id] = true
			if first == "" {
				first = id
			}
			last = id
			if timestamp < start || timestamp > end {
				continue
			}
			candidate, e := openAIListingBind(raw, id, target, pairs)
			if e != nil {
				return fail()
			}
			if seenPairs[candidate.Pair] {
				return fail()
			}
			seenPairs[candidate.Pair] = true
			if candidate.Pair == target {
				result.TargetFound = true
			}
			result.Candidates = append(result.Candidates, candidate)
		}
		for _, field := range []struct{ name, expected string }{{"first_id", first}, {"last_id", last}} {
			value := fields[field.name]
			if len(value) == 0 || len(data) == 0 && string(value) == "null" {
				continue
			}
			id, e := openAIEvidenceID(value)
			if e != nil || id != field.expected {
				return fail()
			}
		}
		after = last
	}
	return result, nil
}

func openAIListingBind(raw []byte, id string, target openAIListingPair, pairs []openAIListingPair) (openAIListingCandidate, error) {
	fail := func() (openAIListingCandidate, error) {
		return openAIListingCandidate{}, fmt.Errorf("invalid candidate evidence")
	}
	fields, err := batchapi.DecodeEvidenceProjection(raw, "endpoint", "input_file_id", "metadata")
	if err != nil {
		return fail()
	}
	endpoint, e := evidenceString(fields["endpoint"])
	file, f := openAIEvidenceID(fields["input_file_id"])
	metadata, m := batchapi.DecodeEvidenceProjection(fields["metadata"], "submission_id")
	sub, s := openAIEvidenceID(metadata["submission_id"])
	if e != nil || endpoint != "/v1/responses" || f != nil || s != nil || m != nil || !openAIUsableID(file) || !openAIUsableID(sub) {
		return fail()
	}
	pair := openAIListingPair{SubmissionID: sub, InputFileID: file}
	if pair != target && (pair.SubmissionID == target.SubmissionID || pair.InputFileID == target.InputFileID) {
		return fail()
	}
	matches := 0
	for _, p := range pairs {
		if p == pair {
			matches++
		}
	}
	if matches != 1 {
		return fail()
	}
	return openAIListingCandidate{BatchID: id, Pair: pair}, nil
}
