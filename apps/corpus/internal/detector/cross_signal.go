package detector

import "danny.vn/trachuan/apps/corpus/internal/stage/relate/textrule"

const (
	RawFieldNotNormalized          = "raw_field_not_normalized"
	RelationKindDisagreement       = "relation_kind_disagreement"
	PartialWithoutProvisionEffect  = "partial_without_provision_effect"
	UnmatchedOperativeClaim        = "unmatched_operative_claim"
	WaivedDocumentWithoutRelations = "waived_document_without_relations"
	ExternalReport                 = "external_report"
	OCRLabelItemOutsideLabels      = "ocr_label_item_outside_labels"
	TreeTranscriptionDivergence    = "tree_transcription_divergence"

	SubjectDocument  = "document"
	SubjectClaim     = "claim"
	SubjectRelation  = "relation"
	SubjectReport    = "report"
	SubjectCheckItem = "check_item"

	DiscriminatorNone  = "none"
	DiscriminatorField = "field"

	CompletenessDocumentObserved = "document_in_release_not_baseline_served"
	CompletenessPairRead         = "source_text_fully_read_and_target_in_release"
	CompletenessClaimRead        = "source_article_read_and_every_target_resolved"
	CompletenessOCRDocument      = "ocr_document_without_extract_gap_not_baseline_served"
	CompletenessReportsFile      = "reports_file_of_the_build_artifact_read"
	CompletenessOCRCheckItem     = "same_document_checked_item_or_complete_absence"
	CompletenessTreePair         = "measured_accepted_pair_or_conclusive_exclusion"
)

func crossSignal() []Detector {
	return []Detector{
		{
			ID: RawFieldNotNormalized, Version: 1, Semantics: 1, Subject: SubjectDocument,
			Inputs: []string{"release.document", "release.coverage_gap"}, Severity: Info,
			Fingerprint:   []string{"field", "raw_sha256", "normalized_sha256"},
			Discriminator: DiscriminatorField, Completeness: CompletenessDocumentObserved,
		},
		{
			ID: PartialWithoutProvisionEffect, Version: 1, Semantics: 1, Subject: SubjectDocument,
			Inputs: []string{"release.document", "release.relation", "release.coverage_gap"}, Severity: Info,
			Fingerprint:   []string{"status_class", "provision_target_relations"},
			Discriminator: DiscriminatorNone, Completeness: CompletenessDocumentObserved,
		},
		{
			ID: RelationKindDisagreement, Version: 1, Semantics: 1, Subject: SubjectRelation,
			Inputs:        []string{"release.document", "release.relation", "release.provision", "release.coverage_gap"},
			Severity:      Review,
			Fingerprint:   []string{"text_kind", "diagram_kinds"},
			Discriminator: DiscriminatorNone, Completeness: CompletenessPairRead,
		},
		{
			ID: UnmatchedOperativeClaim, Version: 1, Semantics: 1, Subject: SubjectClaim,
			Inputs:        []string{"release.document", "release.provision", "release.relation", "release.coverage_gap", "run.snapshot.central_issuers", "text_rules:" + textrule.RulesVersion},
			Severity:      Review,
			Fingerprint:   []string{"source_class", "kind", "sentence_sha256", "unmatched_targets"},
			Discriminator: DiscriminatorNone, Completeness: CompletenessClaimRead,
		},
		{
			ID: WaivedDocumentWithoutRelations, Version: 1, Semantics: 1, Subject: SubjectDocument,
			Inputs: []string{"release.document", "release.relation", "release.coverage_gap", "extract.text.json"}, Severity: Info,
			Fingerprint:   []string{"waiver_check_sha256", "extract_sha256"},
			Discriminator: DiscriminatorNone, Completeness: CompletenessOCRDocument,
		},
		{
			ID: OCRLabelItemOutsideLabels, Version: 1, Semantics: 1, Subject: SubjectCheckItem,
			Inputs: []string{"build.inputs.json", "release.document", "extract.detector_inputs"}, Severity: Review,
			Fingerprint:   []string{"pdf_sha256", "text_sha256", "engine_id", "checker_id", "verdict", "ocr_reading_sha256", "pdf_reading_sha256", "tail_scope_index", "start_paragraph", "end_paragraph"},
			Discriminator: DiscriminatorNone, Completeness: CompletenessOCRCheckItem,
		},
		{
			ID: TreeTranscriptionDivergence, Version: 1, Semantics: 1, Subject: SubjectDocument,
			Inputs: []string{"build.inputs.json", "release.document", "extract.detector_inputs"}, Severity: Info,
			Fingerprint:   []string{"tree_chars", "transcription_chars", "tree_text_sha256", "transcription_text_sha256"},
			Discriminator: DiscriminatorNone, Completeness: CompletenessTreePair,
		},
		{
			ID: ExternalReport, Version: 1, Semantics: 1, Subject: SubjectReport,
			Inputs: []string{"build.reports.yaml"}, Severity: Review,
			Fingerprint:   []string{"kind", "target", "claim"},
			Discriminator: DiscriminatorNone, Completeness: CompletenessReportsFile,
		},
	}
}

func CrossSignalIDs() []string {
	specs := crossSignal()
	ids := make([]string, len(specs))
	for i, d := range specs {
		ids[i] = d.ID
	}
	return ids
}
