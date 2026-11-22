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
| `article_label`   |           0 |           396 |             14 |             8 |              1 |
| `document_number` |           0 |           120 |              5 |            28 |              1 |
| `amount`          |           0 |            36 |              0 |             0 |              0 |
| `date`            |           0 |            23 |              0 |             0 |              0 |
| `extra_text`      |           0 |            96 |              1 |             0 |              0 |
| `missing_text`    |           0 |           126 |             19 |             0 |              0 |
| Total             |           0 |           797 |             39 |            36 |              2 |

38 of 535 candidate items would fire across 25 documents. Of the remainder, 283
had matches outside tail only, 212 had no classifiable main span, and two
matched both inside and outside tail. Eight firing items matched more than once,
with every match in tail. Requiring the main article sequence to pass would cut
the queue to nine items, losing 29 with a parsed tail. `142/2026/NĐ-CP` fires
for five items even though its OCR was served; `1528/QĐ-TTg` fires for one
withheld item. These are location signals, not new readings of the PDFs.

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

The session scratchpad contains `reports/detectors/run.sh`,
`reports/detectors/measure_test.go` (the exact `SELECT` queries and Go probe),
`reports/detectors/overlay.json`, and `reports/detectors/distribution.py`.
`run.sh` uses the Go overlay, without installing a source file in the checkout,
and writes no database rows. The query joins `run_item` and `artifact` for
committed fetch and extract inputs, checks the extract hash against the release
`document` row, and checks the observed `tree_text_partial` gaps. The OCR probe
reads `run_ocr_selection`, `ocr_text`, and `ocr_check` and checks OCR text
SHA-256 on disk. The output is `reports/detectors/measurement.log` with numeric
per-document tree observations in `reports/detectors/tree.tsv`.

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
