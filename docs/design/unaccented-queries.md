# Search without Vietnamese diacritics

Retrieval recipe 4 uses the release's own legal text to restore diacritics
before embedding. In the
[local study](../research/unaccented-queries-2026-11.md), form D used the
adjacent-pair path on 134 folded questions and measured recall at 8 of 0.857. A
per-syllable vote reached 0.835 and the accented originals reached 0.875. The
study restored 14 questions that contained a valid document number after folding
(4 legacy and 10 development). Recipe 4 searches those as typed. The study did
not measure this routing rule, candidate eligibility on explicit or
cursor-pinned dates, the 4,096-code-point prefix, the eight-option cap or the
closed scoring alphabet.

Every raw folded question in the study was flagged low-confidence. Recipe 4
keeps the hybrid arms and their confidence statistic. A lexical-only route would
need its own confidence rule. Recipe 4 draws every replacement from corpus text.
The errors on `bao nhiêu` and `năm` suggest a fixed list of question words could
help, but the study did not measure one.

## `vn-restore/1`

Run only on the route selected by
[validation](retrieval-recipe.md#validation-and-routing). Encode the original
query with the release's folded BM25 tokenizer. An empty sparse vector has no
candidates. Otherwise run one exact lexical query with the normal positive-score
rule and total order (descending score, then ascending chunk ID), at the
restoration depth in [budgets](budgets.md#retrieval). Use the primary group's
document eligibility at the same resolved date and with the same presence or
absence of `as_of` as final collection. Exclude `relation_context` and the
non-current group. Filters, `in_force_only`, limits, detail, arm modes, and
research eligibility overrides do not change the restoration candidate set.

Read only `chunk.content` for those IDs in the same read-only SQL statement.
Take each bounded prefix inside the database, return a flag when the prefix was
cut, and drop the last token if it reaches a cut boundary. Do not fetch the
context prefix, provisions, candidate vectors, or relations. Text from separate
chunks never forms a pair. An overlapping span counts once per occurrence in the
returned chunks, as in the study.

The scoring alphabet consists of ASCII letters, `đ` and `Đ`, and the following
NFC vowels. Each row pairs lowercase and uppercase code points by position. The
fold of every vowel in a row is its ASCII letter in the last column.

| Lowercase | Uppercase | Fold |
| --------- | --------- | ---- |
| aàáảãạ    | AÀÁẢÃẠ    | a    |
| ăằắẳẵặ    | ĂẰẮẲẴẶ    | a    |
| âầấẩẫậ    | ÂẦẤẨẪẬ    | a    |
| eèéẻẽẹ    | EÈÉẺẼẸ    | e    |
| êềếểễệ    | ÊỀẾỂỄỆ    | e    |
| iìíỉĩị    | IÌÍỈĨỊ    | i    |
| oòóỏõọ    | OÒÓỎÕỌ    | o    |
| ôồốổỗộ    | ÔỒỐỔỖỘ    | o    |
| ơờớởỡợ    | ƠỜỚỞỠỢ    | o    |
| uùúủũụ    | UÙÚỦŨỤ    | u    |
| ưừứửữự    | ƯỪỨỬỮỰ    | u    |
| yỳýỷỹỵ    | YỲÝỶỸỴ    | y    |

Map `ABCDEFGHIJKLMNOPQRSTUVWXYZ` to `abcdefghijklmnopqrstuvwxyz` by position and
`Đ` to `đ`. For folding, also map `đ` to `d`. The remaining lowercase ASCII
letters fold to themselves. A token entirely in the alphabet uses the table's
lowercase scoring form. Score and keep any other token byte for byte, including
digits, other letters and decomposed marks. It supplies no alternative forms and
receives none.

1. Tokenize query and candidate text into maximal runs in the general categories
   Letter, Mark and Number, retaining byte spans in the query. Treat a separator
   as adjacent when every code point is `White_Space` and none is LF, CR, U+2028
   or U+2029. Use the fixed
   [Unicode 17.0.0 character properties](https://www.unicode.org/Public/17.0.0/ucd/)
   for these boundaries. A runtime upgrade must preserve the classifications for
   this recipe.
2. Count each scoring form and each adjacent ordered pair in the candidate text,
   including repeats. For a query token made only of ASCII letters, include its
   typed form and observed forms in the scoring alphabet with the same fold.
   De-duplicate by scoring form before applying the budgeted option cap. Count
   the typed form first and order other forms by descending single count, then
   ascending UTF-8 bytes. A fixed token such as a year can still contribute pair
   evidence when it appears exactly as typed.
3. For each adjacent query run, use dynamic programming with one best path per
   last form. Compare paths first by the greater sum of ordered-pair counts,
   then by the greater sum of single counts, then by the smaller UTF-8 byte
   sequence of the scoring form list joined with U+0000. A missing count is
   zero. Keep backpointers instead of copying path prefixes at each transition.
   Iterate within the restoration deadline. Emit the winning replacements with
   the original ASCII case at each letter position. Copy all untouched tokens
   and intervening bytes exactly, including whitespace and punctuation. The fold
   and character rule preserve token identities for the lexical tokenizer and
   prevent a replacement from adding markup or changing a number.

If no candidate or no replacement exists, search the original bytes through the
ordinary hybrid path. Use normal evidence abstention, including the
[confidence rule](retrieval-validity.md#validity-and-evidence). The rule does
not identify the language, and a restored syllable can be wrong. The response
shows the text used when a replacement occurred so callers can correct it.

## Execution and failure

`Search` validates and resolves the cursor and pins the date before `Prepare`.
Standalone `Prepare` pins the collection date once when the caller supplies no
instant. After validating the original query, request options and served filter
codes, `Prepare` restores once if the release recipe selects the route. It keeps
the original request, and carries the effective text and the original routing
result separately. On the restoration route the ordinary lexical and dense arms
use the effective text. The embedder receives those bytes once and applies its
existing model-token limit. `Collect` reuses the prepared vector and text for
pagination, shaping and evaluation controls. It does not run restoration again.

Restoration adds one lexical query with bounded candidate text and one bounded
in-process pass. Apply the restoration phase deadline to connection acquisition,
the query, the candidate scan and path selection, capped by the caller's
context. Set a transaction-local SQL deadline within that cap. PostgreSQL's
[`statement_timeout`](https://www.postgresql.org/docs/current/runtime-config-client.html#GUC-STATEMENT-TIMEOUT)
applies to individual statements, so the context also bounds the combined phase.
Finish the read-only transaction before calling the embedder. The usual
collection and embedder limits then apply to those phases.

A restoration timeout returns the existing `unavailable` error (HTTP 503 on
`/api/v1`, tool error on MCP), with no hits, cursor or partial restoration. Fail
closed on candidate read errors too, using the existing release-expired error
when the pinned release is gone. An empty candidate set is a successful lookup.
The route adds no retries on serving requests.

The restoration route bypasses response cache reads and writes. A restored query
can still contain names, numbers and other text from the original. The vector
and candidate caches use digests of the effective query under the usual
[cache rules](caching.md). Retain neither the sampled text nor a
query-to-restoration map. Never send the original query, candidate text or
restored text to logs, traces, metric labels or error messages. The caller
receives a restoration as text data, outside the verbatim evidence and
citations.

## Contract tests

Use fixed fixtures to cover a pair win over a single-token winner, ties, caps,
case-variant de-duplication before the cap, unchanged tokens outside the
alphabet, case and separator preservation, and a zero-match search. Test an
accented query and a valid document number with no restoration lookup, recipe 2
and 3 compatibility, and recipe 4 `unsupported_query`. Prove that primary
lexical ordering, date eligibility and cursor pinning use the same release and
date, that filters do not change the restoration, and that a continuation
rejects the edited query. Cover timeouts with no embed, cache write or response
data. Check that the restored field is identical in compact and full results,
MCP and web API, first and later pages, and present on abstentions when the
effective text changed. Check that the web page lets the caller retry edited
text as a new search. For repeats with and without replacements, assert zero
response-cache reads and writes, including when a response cache entry already
exists. Verify that repeated requests can use vector and candidate cache hits
through effective-query digests. These tests use fixed inputs.
