# Queries typed without diacritics (2026-11-26)

Measured on the MacBook on 2026-11-26 to choose a route for Vietnamese queries
typed without diacritics. Not authority; the
[retrieval design](../design/retrieval.md) decides.

## Setup

- Release `vn/20261124_1`, restored locally, exact retrieval, limit 8, default
  validity, native query embedder.
- 134 cases with expected citations from the current `legacy` (84) and
  `development` (50) cohorts, matched with the evaluation package's own matcher.
- A throwaway program ran four forms of each question. It was not merged.

| Form | Query sent to retrieval                                                |
| ---- | ---------------------------------------------------------------------- |
| A    | The question as written, with diacritics                               |
| B    | Folded text (lower case, marks dropped, `đ` to `d`), gate skipped      |
| C    | B with each syllable restored by a vote over the top 20 lexical hits   |
| D    | B restored by the best path over adjacent syllable pairs in those hits |

C and D first run the lexical arm with the folded query, which already matches
folded index terms. They read the text of the top 20 lexical candidates and
collect the accented forms whose fold equals each query syllable. C picks the
most frequent form per syllable. D picks the sequence of forms with the highest
total count of adjacent pairs in those texts, then the highest total count of
single forms, then byte order. Tokens with a digit, or with no folded match,
stay as typed. Pairs do not cross punctuation or line breaks.

## Results

| Form | Answered  | Recall at 8, all cases | Recall at 8, answered | MRR at 8 | Syllables restored exactly |
| ---- | --------- | ---------------------- | --------------------- | -------- | -------------------------- |
| A    | 112 / 134 | 0.875                  | 0.913                 | 0.721    | not applicable             |
| B    | 0 / 134   | 0.178                  | not applicable        | 0.148    | not applicable             |
| C    | 102 / 134 | 0.835                  | 0.886                 | 0.695    | 0.892                      |
| D    | 110 / 134 | 0.857                  | 0.886                 | 0.704    | 0.940                      |

- B: every case abstains with `low_confidence`. The dense arm embeds unaccented
  text and the abstention statistic is a cosine. Skipping the gate is not a
  route.
- C: 13 cases answered in A abstain, and 7 cases found in A are missed. The
  errors are context errors of the per-syllable vote: `thống tin` for
  `thông tin`, `tin dụng` for `tín dụng`, `thông từ` for `thông tư`, `hiệu lục`
  for `hiệu lực`.
- D: 6 cases answered in A abstain, 4 abstained in A are answered, and 4 cases
  found in A are missed. 51 of 134 questions are restored to exactly the
  original text.

## What D still gets wrong

| Typed          | Restored       | Wanted         | Why                                                    |
| -------------- | -------------- | -------------- | ------------------------------------------------------ |
| `la bao nhieu` | `là bảo nhiều` | `là bao nhiêu` | Question words do not occur in legal text              |
| `nam 1951`     | `nam 1951`     | `năm 1951`     | The top hits hold `Việt Nam` far more often than `năm` |
| `tra lai`      | `tra lãi`      | `trả lãi`      | The pair is absent from the top 20 hits                |
| `so du vi`     | `sở dữ vi`     | `số dư ví`     | Same; the cited decree was not among the lexical hits  |

Two of the four missed cases still answer, with the wrong evidence ranked first,
because only question words were restored wrongly.

## Reading

- Restoring diacritics from the release's own text recovers 98% of the recall of
  accented queries (0.857 of 0.875) with one extra lexical query and no model or
  dictionary.
- The remaining errors come from words that the top hits do not hold. A small
  fixed list of question and function words, or pair counts over the whole
  release, are the two candidate improvements. Neither was measured.
- A query in another language keeps the tokens that have no folded match. A
  token that folds to a Vietnamese syllable can still be restored (`ban` to
  `bán`); the cosine threshold then decides abstention.
- 14 of the 134 folded questions (4 legacy, 10 development) pass today's query
  validation because they hold a document number. The program restored those
  like any other question. A route that sends document-number queries as typed
  was not measured.
