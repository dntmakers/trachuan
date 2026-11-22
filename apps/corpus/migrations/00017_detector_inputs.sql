-- +goose Up
ALTER TABLE run_ocr_selection
    DROP CONSTRAINT run_ocr_selection_check,
    DROP CONSTRAINT run_ocr_selection_check1,
    ADD COLUMN selection_version integer NOT NULL DEFAULT 0,
    ADD COLUMN selection_state text,
    ADD COLUMN text_source text,
    ADD COLUMN fetch_sha256 text CHECK (fetch_sha256 ~ '^[0-9a-f]{64}$'),
    ADD CONSTRAINT run_ocr_selection_versioned_check CHECK (
        CASE selection_version
        WHEN 0 THEN
            selection_state IS NULL AND text_source IS NULL AND fetch_sha256 IS NULL
            AND ((gap = '') = (ocr_text_id IS NOT NULL AND ocr_check_id IS NOT NULL AND pdf_sha256 IS NOT NULL))
            AND (gap <> '' OR ocr_check_id IS NOT NULL)
        WHEN 1 THEN
            fetch_sha256 IS NOT NULL AND selection_state IS NOT NULL AND text_source IS NOT NULL
            AND CASE selection_state
                WHEN 'checked' THEN
                    pdf_sha256 IS NOT NULL AND ocr_text_id IS NOT NULL AND ocr_check_id IS NOT NULL
                    AND ((text_source = 'ocr' AND gap = '')
                         OR (text_source = 'none' AND gap IN ('ocr_rejected', 'ocr_label_flagged'))
                         OR (text_source = 'source_tree' AND gap = 'tree_headings_only'))
                WHEN 'unavailable' THEN
                    pdf_sha256 IS NULL AND ocr_text_id IS NULL AND ocr_check_id IS NULL
                    AND ((text_source = 'none' AND gap IN ('needs_ocr', 'text_source_ambiguous'))
                         OR (text_source = 'source_tree' AND gap = 'tree_headings_only'))
                WHEN 'not_selected' THEN
                    pdf_sha256 IS NULL AND ocr_text_id IS NULL AND ocr_check_id IS NULL
                    AND ((text_source = 'none' AND gap = 'text_number_mismatch')
                         OR (text_source = 'source_tree' AND gap = 'tree_headings_only'))
                ELSE false
            END
        ELSE false
        END
    );

UPDATE run SET snapshot = jsonb_set(snapshot, '{extract_record_contract}', '"legacy"'::jsonb)
WHERE snapshot->'Recipes' ? 'extract' AND NOT snapshot ? 'extract_record_contract';

-- +goose Down
ALTER TABLE run_ocr_selection
    DROP CONSTRAINT run_ocr_selection_versioned_check,
    ADD CHECK ((gap = '') = (ocr_text_id IS NOT NULL AND ocr_check_id IS NOT NULL AND pdf_sha256 IS NOT NULL)),
    ADD CHECK (gap <> '' OR ocr_check_id IS NOT NULL),
    DROP COLUMN selection_version,
    DROP COLUMN selection_state,
    DROP COLUMN text_source,
    DROP COLUMN fetch_sha256;
UPDATE run SET snapshot = snapshot - 'extract_record_contract';
