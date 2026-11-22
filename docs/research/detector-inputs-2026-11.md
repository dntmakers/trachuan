# Text detector inputs on vn/20261121_1

Measured 2026-11-22 for the [text detectors](../design/detector-inputs.md). The
release run had 3,847 done extracts. The measurement used PostgreSQL 18.7, the
committed extract and fetch bundles, and the local OCR store. It made no writes
to either database and no external calls.

## OCR check items

The workshop had 316 OCR selections on the run. Of these, 104 still bind a
served text and completed check, 190 withheld text for `ocr_label_flagged`, and
nine withheld it for `ocr_rejected`. Eight `needs_ocr`, three
`text_source_ambiguous`, and two `text_number_mismatch` selections did not
supply a check for the measurement.

The extract recipe's checker, `claude-opus-5-5/medium/ocr-check-2`, yielded 303
document checks: 56 clean, 238 flagged and nine rejected. For the 104 served
selections, the script matched the frozen check id and text and result hashes.
For the 199 withheld selections, the old run has no ids to verify: the script
reconstructed the candidate with extract's `SelectCheckSource` from the frozen
fetch bundle and the local store as measured. Those counts are a replay
estimate, not proof of which check ids the old run used. The script did not
treat checks added under other checker ids as run inputs.

| Item kind         | Clean items | Flagged items | Rejected items | Flagged fires | Rejected fires |
| ----------------- | ----------: | ------------: | -------------: | ------------: | -------------: |
| `article_label`   |           0 |           396 |             14 |             9 |              1 |
| `document_number` |           0 |           120 |              5 |            28 |              1 |
| `amount`          |           0 |            36 |              0 |             0 |              0 |
| `date`            |           0 |            23 |              0 |             0 |              0 |
| `extra_text`      |           0 |            96 |              1 |             0 |              0 |
| `missing_text`    |           0 |           126 |             19 |             0 |              0 |
| Total             |           0 |           797 |             39 |            37 |              2 |

39 of 535 candidate items would fire across 25 documents. Of the remainder, 283
had complete token matches outside tail only, 212 had no classifiable main span,
and one remained ambiguous. Eight firing items had more than one complete match
in tail. No item was unlocated because all matches were embedded, or because the
scan reached its work bound.

The whole-token rule raised the estimate from 38 to 39: one item for
`30/VBHN-NHNN` became tail-only after embedded matches were ignored. Requiring
the main article sequence to pass would cut the queue to nine items.
`142/2026/NĐ-CP` fires for five items even though its OCR was served;
`1528/QĐ-TTg` fires for one withheld item. These are location signals, not new
readings of the PDFs.

## Tree and transcription volume

The measurement used the production partial-tree function to compute Unicode
code point counts `T` for the rendered tree and `H` for the transcription
paragraphs. 3,280 pairs had positive counts, including 1,933 with the tree
retained, 127 partial-tree switches, 338 heading-agreement switches and 882
other HTML selections. Of the other extracts, 552 had no tree, nine were
non-VBPL, two served neither tree nor HTML, and four pairs were unmeasurable.

| Cohort                     |     N |     min |    p05 |    p25 |    p50 |    p75 |    p95 |    p99 |    max |
| -------------------------- | ----: | ------: | -----: | -----: | -----: | -----: | -----: | -----: | -----: |
| All pairs, `T/H`           | 3,280 | 0.00018 | 0.0287 | 0.2539 | 0.7013 | 0.9254 | 0.9956 | 1.2199 | 1.8655 |
| Retained, valid main `T/H` | 1,043 |  0.5005 | 0.5843 | 0.8075 | 0.9160 | 0.9706 | 1.0511 | 1.3444 | 1.7472 |

Quantiles use the nearest index in the sorted list (`round(p × (n - 1))`). The
rows below use strict `min(T,H)/max(T,H) < threshold`, with exact integer
comparisons. The retained cohort with an accepted main transcription scope
contains no existing `tree_text_partial` gaps; 155 other retained trees have
that gap.

| Threshold | All pairs flagged | Eligible: tree shorter | Eligible: HTML shorter | Eligible total |
| --------- | ----------------: | ---------------------: | ---------------------: | -------------: |
| `1/2`     |             1,202 |                      0 |                      0 |              0 |
| `3/5`     |             1,433 |                     59 |                      1 |             60 |
| `2/3`     |             1,572 |                    110 |                      4 |            114 |
| `3/4`     |             1,780 |                    186 |                     11 |            197 |
| `4/5`     |             1,917 |                    252 |                     16 |            268 |

`3/5` is the smallest tested threshold with new signals in both directions. At
that threshold 5.8% of the eligible cohort fires. There was no content
adjudication: volume disagreement alone does not establish a defect.

| Number          | Tree chars | HTML chars | Fires at `3/5`              |
| --------------- | ---------: | ---------: | --------------------------- |
| 21/2011/TT-NHNN |        919 |      1,836 | Yes, tree shorter           |
| 50/2026/TT-NHNN |     92,439 |    183,441 | Yes, tree shorter           |
| 17/2013/NĐ-CP   |     11,526 |      6,597 | Yes, HTML shorter           |
| 16/2013/TT-BTC  |     48,442 |     29,563 | No, above threshold         |
| 42/2014/TT-NHNN |        923 |    150,533 | No, already serves the HTML |

## Reproduction

Use `workshop` and `corpus_vn_20261121_1` on local PostgreSQL with
`default_transaction_read_only=on`. Use the artifact root for the recorded paths
and the OCR store for `text/<text_sha256>.txt`. Run the SQL below with read
privileges in the named databases.

```sql
-- Release database: document inventory.
SELECT d.key, d.number, d.text_source, COALESCE(d.extract_sha256, ''),
       EXISTS (SELECT FROM coverage_gap g WHERE g.stage = 'build'
               AND g.kind = 'baseline_served' AND g.item_key = d.key),
       d.is_consolidated
FROM document d ORDER BY d.key;

-- Workshop: committed fetch/extract pairs and their frozen OCR decision.
SELECT i.item_key, fa.path, fa.sha256, ea.path, ea.sha256, COALESCE(o.gap, ''),
       o.item_key IS NOT NULL, COALESCE(o.ocr_check_id, 0)
FROM run r
JOIN run_item i ON i.run_id = r.id AND i.stage = 'extract' AND i.state = 'done'
JOIN artifact ea ON ea.id = i.artifact_id
JOIN run_item f ON f.run_id = r.id AND f.item_key = i.item_key
               AND f.stage = 'fetch' AND f.state = 'done'
JOIN artifact fa ON fa.id = f.artifact_id
LEFT JOIN run_ocr_selection o ON o.run_id = r.id AND o.item_key = i.item_key
WHERE r.country = 'vn' AND r.rel = '20261121_1'
ORDER BY i.item_key;

-- Workshop: coverage gap for each paired document key ($1).
SELECT EXISTS (
    SELECT FROM gap g JOIN run r ON r.id = g.run_id
    WHERE r.country = 'vn' AND r.rel = '20261121_1' AND g.stage = 'extract'
      AND g.item_key = $1 AND g.kind = 'tree_text_partial'
);

-- Workshop: OCR store lookup by array of candidate PDF digests ($1).
SELECT id, pdf_sha256, engine_id, text_sha256, bytes, origin, pages, created_at
FROM ocr_text WHERE pdf_sha256 = ANY($1::text[])
ORDER BY pdf_sha256, engine_id;

-- Workshop: completed result for the OCR text id ($1) and checker id ($2).
SELECT id, ocr_text_id, checker_id, result_sha256, result, flagged, created_at
FROM ocr_check WHERE ocr_text_id = $1 AND checker_id = $2;
```

1. Iterate the workshop item rows in key order. Skip absent and baseline release
   rows; require the extract digest to equal the release row's digest. Verify
   any `text.json` read against the committed extract manifest.
2. For each VBPL document, verify and decode the fetch `tree.json`. Where the
   tree is nonempty and the release serves tree or HTML, use `selectFiles` on
   that fetch bundle with OCR disabled. Verify the file list and the selected
   HTML unless it exceeds the size limit. Render the tree from `extractTree`;
   use `partialTreeLengths` on those rows and the selected HTML. Keep usable
   pairs with positive counts. Record `text_source`, accepted main scope,
   existing `tree.rule` (or heading-agreement if `tree.set_aside` is true
   without a rule), both counts and the `tree_text_partial` gap. For a
   `partial_tree` rule assert that the counts equal the frozen `tree` record.
   Bin by serving source and rule, treating HTML without a tree decision as
   other HTML. Sort ratios for quantiles. For threshold `n/d`, count a shorter
   tree if `d*T < n*H`, or shorter HTML if `d*H < n*T`.
3. For OCR, include only workshop rows whose selection gap is empty,
   `ocr_label_flagged` or `ocr_rejected`. Use `SelectCheckSource` on the frozen
   fetch bundle with the checker named above and extract's engine order. The
   function consults OCR only after the usual source selection. Select the one
   distinct PDF or the only PDF with a store entry; choose the first installed
   engine in `AcceptedEngines` order. Obtain the check with `GetOCRCheck`. For a
   served selection require matching frozen check id and `text.json` text and
   result digests. Decode with `DecodeCheck`, verify the stored OCR file with
   `ReadText`, count its verdict and tabulate all item kinds per verdict.
4. Parse the verified OCR with `ParagraphsFromText` and `ParseArticles`. Map
   each paragraph ordinal to `ScopeIndex` using each provision's inclusive
   ordinal span; unassigned paragraphs are outside tail. An item cannot fire if
   `same_document` is false or the parser recognizes no main span. Join
   paragraphs with a space and use the
   [specified matching and work bound](../design/detector-inputs.md#locate-the-reading-in-the-stored-ocr-text),
   caching classifications by NFC, whitespace-collapsed OCR reading. The probe
   used Knuth–Morris–Pratt, binary search on paragraph start offsets, and a
   prefix count of tail paragraphs to test span coverage. Count one fire per
   tail-only item and one firing document per unique key. Also count physical
   matches before the token boundary test to identify an embedded-only reading.

Production functions at `7427e297`:

- Bundle, tree and length measurement: `selectFiles`, `extractTree` and
  `partialTreeLengths`
  ([selection.go:42–79](../../apps/corpus/internal/stage/extract/selection.go#L42-L79),
  [tree.go:24](../../apps/corpus/internal/stage/extract/tree.go#L24),
  [tree_text_partial.go:30–61](../../apps/corpus/internal/stage/extract/tree_text_partial.go#L30-L61)).
- OCR selection and scope reading: `SelectCheckSource`, `ReadText`,
  `ParagraphsFromText`, `ParseArticles`
  ([check_source.go:38–48](../../apps/corpus/internal/stage/extract/check_source.go#L38-L48),
  [store.go:49–69](../../apps/corpus/internal/ocrstore/store.go#L49-L69),
  [paragraphs.go:32–45](../../apps/corpus/internal/textparse/paragraphs.go#L32-L45),
  [parse.go:17–95](../../apps/corpus/internal/textparse/parse.go#L17-L95)).
