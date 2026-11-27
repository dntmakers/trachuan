# Retrieval and the MCP interface

One engine, two faces: `/api/v1/*` for the website (cookie session and CSRF
token) and `/mcp` for agents (bearer token). Same functions, rules and limits.

## Query validation

Accept Vietnamese with or without diacritics and recognize document numbers such
as `50/2024/TT-NHNN`. The release's
[retrieval recipe](retrieval-recipe.md#validation-and-routing) decides
validation and routing. Evidence-based abstention handles accepted queries
outside the corpus. Legal text stays in its original language.

## Ranking

The shared Go engine implements the [retrieval recipe](retrieval-recipe.md):
dense cosine and BM25, weighted reciprocal rank fusion, provision grouping,
document diversity and explicit abstention. [Budgets](budgets.md#retrieval) owns
parameters; [evaluation](evaluation.md) owns calibration and measurement. The
first recipe is a baseline, with a provisional old-product threshold; the
development cohort precedes recipe changes under ADR 0010 item 4.

## Validity

`in_force_only` defaults to true. Document dates, missing-date gaps, historical
filtering and the separate non-current group follow the
[retrieval recipe](retrieval-validity.md#validity-and-evidence). Provision-level
repeal relations appear as warnings, never as a changed stamp.

## Filter contract

Filters reorder, never hide. `nodes[]`, `doc_types[]`, `issuers[]`,
`documents[]` and the issue-date range are ordering preferences. Together they
form one predicate: a hit is inside when it matches every kind the request
names, and a list kind matches when any of its values does. A node matches when
the hit provision has an effective node in that node's subtree
([taxonomy](taxonomy.md#serving)); a node code that is not served is
`invalid_request`. `doc_types` and `issuers` match the hit document's `type` and
`issuer` after trimming and NFC normalization; `documents` match document keys.
`corpus_status` lists the release's normalized document types and issuers with
document counts over the classes search ranks by default (all but
`relation_context`), so callers pick values that exist. It needs no date range:
the bounds are dates a caller chooses, not codes from a release vocabulary.

`issued_from` and `issued_to` are inclusive bounds on the document's stored
`issue_date`; either bound alone is valid. If either is present, a document with
a null issue date is outside the filter. If neither is present, issue date does
not affect membership. Each present value must be a Gregorian civil date from
year 0001 to 9999 in `YYYY-MM-DD`
([RFC 3339 full-date](https://www.rfc-editor.org/rfc/rfc3339.html#section-5.6));
reject nulls, empty strings, malformed or impossible dates and a lower bound
after the upper bound as `invalid_request` before retrieval. These bounds do not
affect `as_of`, eligibility, status, candidate collection or validity stamps.

Retrieval always runs over the whole country corpus. Paging walks one list in
two groups, primary and non-current. Each group's sequence is the unfiltered
walk: that group's hits on today's response of the first page's `limit`, then
the rest of the recipe's hits of that group shaped at the list cap of 100, in
rank order, without those already listed. Each page holds at most `limit` hits,
split into `hits` and `non_current` by group. Without filters the first page is
exactly today's response of that `limit`. Every other page fills from what
remains of each sequence: up to the reserved non-current count from the
non-current group, the effective non-current tail with `in_force_only` true and
half of `limit` rounded down with `in_force_only` false
([retrieval recipe](retrieval-recipe.md)), and the rest from primary; in both
modes either group then takes the slots the other cannot fill. When any filter
is given, each sequence is reordered stably, inside hits first, then outside
hits; the filtered first page takes as many hits from each group as today's
first page does and fills any slots left by the same rule, so every inside hit
of today's first page stays on it; every hit carries `in_filter: true|false`;
and `outside_filter_count` counts outside hits over both groups. Every page
carries the first page's `abstain`, `reason`, `message` and `restored_query`
when present. Filters and paging never change them. `next_cursor` continues
while either sequence has hits left, so every hit the unfiltered search would
return stays reachable exactly once. The cursor is opaque and holds the release,
the first page's `limit`, the number of hits taken so far from each sequence,
the pinned retrieval date and a SHA-256 digest of the request without `cursor`
and `limit`. The question appears only in the digest, which binds its exact
original bytes. Whether the caller supplied `as_of` comes from the original
request and participates in the digest separately from the pinned date. An
edited query starts a fresh search. Both issue-date bounds participate in the
digest: adding, removing or changing one requires a new first page. Later pages
use the cursor's date and rebuild the sequences from it, and a cursor whose
digest or release does not match is `invalid_request` or `release_expired`.

The [taxonomy recall gate](evaluation.md#metrics) stays unchanged. Issue dates
add no evaluation cohort or release gate. Instead, contract tests for MCP and
web search use fixed fixtures to prove inside-first stable order, `in_filter`,
`outside_filter_count`, null issue dates, and reachability on the first and
later pages. Test each bound alone, equal bounds, malformed and reversed ranges,
combinations with other filters and `as_of`, and rejection when changing either
bound changes the cursor's request digest.

## Release pinning

Every response carries `release`. `search` and `read` accept an optional
`release`. The server serves only the active release of each country, so a
pinned release that is no longer active returns an explicit `release_expired`
error, never silently different text.

## MCP tools

| Tool            | Input                                                                                                                                                                  | Output                                                                                                                                                                                                                                                                                                                                                                                                                                                                                            |
| --------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `guide`         | `country`                                                                                                                                                              | Usage, Vietnamese rule, citation format, the reading flow in [search results](mcp-results.md#guide), suggested flow `taxonomy` then `search` with `nodes` then `read`, `in_force_only:false` for a past period, `abstain` advisory                                                                                                                                                                                                                                                                |
| `corpus_status` | `country`                                                                                                                                                              | Release, counts of documents, provisions, served nodes and gaps, newest source date, document types and issuers with counts                                                                                                                                                                                                                                                                                                                                                                       |
| `taxonomy`      | `country`, `node` code or empty, `depth`, optional `q`                                                                                                                 | Served subtree in sibling order with `code`, `name`, `description`, `level`, `origin`, provision and document counts, to `depth` levels below `node` (roots when empty); `q` instead returns served nodes in `node`'s subtree (all when empty) whose name or description contains it, case and diacritics folded, as a flat list in tree order. Either way at most 500 nodes: the subtree fills breadth first in sibling order, the `q` list in tree order, and `truncated` marks that more exist |
| `search`        | `country`, `query`, `nodes[]`, `as_of`, `in_force_only`, `doc_types[]`, `issuers[]`, `documents[]`, `issued_from`, `issued_to`, `detail`, `limit`, `cursor`, `release` | `documents` once per document with metadata, status and relation counts (related pointers in full results); ranked hits with handle, citation path, verbatim snippet and `complete`, nodes, warnings, `in_filter`; `detail:"full"` returns recipe text, fragments, score and relations instead of the snippet; plus `outside_filter_count`, `gaps[]`, `abstain`, `restored_query`, `release`, `next_cursor` ([search results](mcp-results.md))                                                    |
| `read`          | `country`, `items[]` of `handle`, number, `from`/`to` range, `relations`, `timeline`, or `next` token; `release`                                                       | Per item: a document's metadata, source files and outline, a provision's verbatim text with ancestor lead-ins and warnings, its relation list, or a document timeline; a per-call cap with fixed parts and opaque `next` tokens ([batch reading](mcp-read.md))                                                                                                                                                                                                                                    |
| `quality_gaps`  | `country`, `kind`, `node`, `cursor`                                                                                                                                    | Paged gap list                                                                                                                                                                                                                                                                                                                                                                                                                                                                                    |

`quality_gaps` with `node` (a taxonomy code, as in `taxonomy`) returns only gaps
whose taxonomy node is in that node's subtree, or whose document belongs to that
subtree the way the `search` `nodes[]` filter decides membership; global gaps
with neither are left out. An unknown code is `invalid_request`. Paging, the
`kind` filter and the page size stay as without `node`.

The proposed `changes` tool lists [release changes](release-changes-serving.md)
under the same release pinning rule.

Tool schemas grow only additively, with one recorded exception for
[release changes](release-changes-encoding.md#version-2). `/mcp` requires scope
`mcp:read` and serves RFC 9728 metadata at
`/.well-known/oauth-protected-resource`. Every `/mcp` response carries
`Cache-Control: no-store, no-transform`, whatever the tool or corpus, so no
separate reading of the request decides it; `no-transform` keeps intermediaries
from compressing or rewriting streamed responses.

## Web API

`POST /api/v1/search`, `GET /api/v1/public/taxonomy`,
`GET /api/v1/documents/{key}`, auth and account endpoints
([web API](web-api.md)). The OpenAPI document in `packages/schema` is the
contract; the Nuxt client is generated from it and CI fails on drift. Limits are
in [budgets](budgets.md).
