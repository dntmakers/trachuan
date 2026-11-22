package fetch

type DetectorInputsV1 struct {
	Version           int                 `json:"version"`
	OCRCheck          OCRCheckObservation `json:"ocr_check"`
	TreeTranscription TreeTranscriptionV1 `json:"tree_transcription"`
}

type OCRCheckObservation struct {
	State string `json:"state"`
	*OCRCheckDetails
}

type OCRCheckDetails struct {
	PDFSHA256      string         `json:"pdf_sha256"`
	EngineID       string         `json:"engine_id"`
	TextSHA256     string         `json:"text_sha256"`
	CheckerID      string         `json:"checker_id"`
	ResultSHA256   string         `json:"result_sha256"`
	SameDocument   bool           `json:"same_document"`
	Verdict        string         `json:"verdict"`
	HasMissingText bool           `json:"has_missing_text"`
	PageCount      int            `json:"page_count"`
	ParagraphCount int            `json:"paragraph_count"`
	LineCount      int            `json:"line_count"`
	ScopeCount     int            `json:"scope_count"`
	OtherItems     int            `json:"other_items"`
	Items          []OCRCheckItem `json:"items"`
}

type OCRCheckItem struct {
	Kind             string       `json:"kind"`
	Page             int          `json:"page"`
	Ordinal          int          `json:"ordinal"`
	OCRReadingSHA256 string       `json:"ocr_reading_sha256"`
	PDFReadingSHA256 string       `json:"pdf_reading_sha256"`
	Position         string       `json:"position"`
	Location         *OCRLocation `json:"location,omitempty"`
}

type OCRLocation struct {
	TailScopeIndex int `json:"tail_scope_index"`
	StartParagraph int `json:"start_paragraph"`
	EndParagraph   int `json:"end_paragraph"`
	StartLine      int `json:"start_line"`
	EndLine        int `json:"end_line"`
}

type TreeTranscriptionV1 struct {
	State string `json:"state"`
	*TreeTranscriptionDetails
}

type TreeTranscriptionDetails struct {
	TreeChars               int    `json:"tree_chars"`
	TranscriptionChars      int    `json:"transcription_chars"`
	TreeTextSHA256          string `json:"tree_text_sha256"`
	TranscriptionTextSHA256 string `json:"transcription_text_sha256"`
	TranscriptionFileSHA256 string `json:"transcription_file_sha256"`
	MainAccepted            bool   `json:"main_accepted"`
}
