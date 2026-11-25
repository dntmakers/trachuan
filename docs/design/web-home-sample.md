# Public home sample

The home page shows one real search excerpt without making arbitrary search
public. [ADR 0005](../adr/0005-google-login-and-embedded-oauth.md#history)
approves this exception. A source excerpt changes with the corpus rather than
with the next web deploy. Search, full text and every other read mode still
require an account.

## Request and published result

`GET /api/v1/public/sample` takes no input. Reject a query string or a request
body with 400 `invalid_request` before release resolution. Headers, cookies,
identity, host, language, path variants and client address never choose the
query, release, search mode or cache key. Only `GET` can start work. The handler
reads no credentials or account tables and sets no cookie. It uses the existing
[search recipe](retrieval-recipe.md) on the active `vn` release, with
`detail:"compact"`, `limit:1`, `in_force_only:true`, no filters or explicit
`as_of` and the following design-owned query constant:

> Ngân hàng cần xin sự đồng ý của khách hàng khi xử lý dữ liệu cá nhân trong
> những trường hợp nào?

This is the authored evaluation fixture `pdpl-consent-91-2025` in
[`legacy.json`](../../apps/corpus/internal/evaluation/testdata/vn/legacy.json),
not a stored user question under
[ADR 0002](../adr/0002-evidence-only-vietnamese-queries.md). A test binds the
constant to the fixture; the runtime does not open the fixture or accept a
configuration override. Do not log queries or result text.

The first primary hit must be from `vn:vbpl:179252`, the law numbered
`91/2025/QH15`; do not pick another hit. The
[snippet rule](mcp-results.md#snippet-rule) must keep and omit non-whitespace
body text of the provision named by the hit's handle. Prove this from the
snippet builder's retained source ranges and the hit's `body` fragments with
that exact provision identity. Count a source position as omitted only if no
retained range contains a copy of it. A cut only in a lead, a heading or a child
provision does not qualify. Neither `complete:false`, a printed omission marker
nor source incompleteness proves a body cut. If the builder cannot prove the cut
from its source spans, use the stored sample.

A 200 response is `PublicSampleResult`: it keeps the stored sample's JSON layout
but has its own OpenAPI schema, not `SearchResult` or its nested schemas. Every
object rejects additional properties. The server constructs this object from
allowed fields, never by serializing a search result:

- Root: required `release` (the active release id), `abstain` (constant
  `false`), `hits` (exactly one item) and `documents`.
- Hit: required `handle` (a provision handle under `vn:vbpl:179252`), `doc`
  (constant `vn:vbpl:179252`), `citation`, `snippet` (nonempty and within the
  snippet cap) and `complete` (constant `false`).
- Documents: exactly one property, `vn:vbpl:179252`. Its value requires `number`
  (constant `91/2025/QH15`), `status`, `text_source` and `text_complete`. It may
  carry `title`, `type`, `issuer`, `issued`, `effective_from`, `effective_to`
  and `source_url`. The URL may be only the canonical
  `https://vbpl.vn/van-ban/chi-tiet/179252`, when the stored URL equals it. The
  closed status object requires boolean `current` and may carry `class`, `basis`
  and `status_basis`.

Use normal omissions for unknown optional values. No path headings, extra or
non-current hits, full text, lead fields, relation evidence, warnings, gaps,
files, hashes or account fields leave the projector. If the projection cannot
meet the schema or the [body cap](budgets.md#request-limits), publish nothing
live.

## Admission, activation and caching

Use the
[public admission gate](web-public-documents-serving.md#admission-and-capacity)
through writing the response, cache hits included. The sample has its own lower
cap inside the shared gate so it cannot occupy all summary slots, and uses no
per-address bucket. An admission failure gives 503 `busy` with the summary
route's `Retry-After`. The sample flag `SERVER_PUBLIC_SAMPLE` defaults off and
is independent of the summary flag. Off gives 404 `not_found` before admission.
A metadata-only lookup identifies the active activation by `active_db` and
`activated_at`, not by release name alone. Missing active release or resolution
failure gives 503 `unavailable`.

The first admitted request without a result starts one search for that
activation, under the [sample budget](budgets.md#request-limits). Concurrent
admitted requests may join that flight, without starting their own. Disconnect
ends a caller's wait, not the bounded work; shutdown or a changed activation
cancels the work. Only one job can run process-wide, including at rollover:
return `busy` if the previous job has not yet freed its slot, without using up
the new activation's attempt. Use the active release's already-open retrieval
engine and postings, with the sample's own nonwaiting read-only database
capacity, the existing embedder admission, and no serving retries. Do not load a
second index or take connections from the reader or summary pools. If the engine
is not ready, return `busy` without starting an attempt; the sample must not
open the engine or delay activation.

Capture the Vietnam civil date from the injected clock at the start of the
attempt, with the usual default validity rule, and freeze it for the request and
its result. Apply metadata observations in read order. Recheck before storing or
replying after a flight; discard a late completion from an earlier activation,
even if a rollback reused its release id. A cache hit always checks the current
activation. The memory cache holds one projected result and its date for the
active activation only. Return that date in `X-Sample-Status-Date` with 200 as
`YYYY-MM-DD`. The live result is a dated snapshot, not a fresh validity stamp
for each visitor. Activation, including rollback, or process restart clears the
slot. No cache file, database write, ordinary search response cache entry or
warm-up task is needed.

On abstention, no usable hit, embedder outage, timeout or other computation
failure, remember only an unavailable marker for that activation and give 503
`unavailable`. One failed attempt does not become repeated anonymous model work.
The stored sample stays available until a new activation or process restart
allows another attempt. Fixed error codes and reason categories in metrics
identify the fallback; do not expose diagnostic text. Return
`Cache-Control: no-store` with every status, without an ETag or Last-Modified,
so intermediaries cannot reuse an old active release after a switch
([RFC 9111](https://www.rfc-editor.org/rfc/rfc9111.html#section-5.2.2.5)).

## Home page and rollout

Fetch the sample from Nitro only, without forwarding headers or credentials from
the visitor. Nitro stops reading the decoded body above the
[body cap](budgets.md#request-limits), even without `Content-Length`. It accepts
only a 200 JSON response that passes runtime validation of `PublicSampleResult`
and has a valid `X-Sample-Status-Date`. No response body goes to the page or its
hydration data before these checks. Read public status after the sample using
the
[home cache and freshness rule](web-public-documents.md#caching-and-freshness).
Use the live result only if its release matches status. Always show
`X-Sample-Status-Date` on the live card, including after Vietnam midnight.

A mismatch after the normal status refresh, a fetch error or a failed check uses
`apps/web/app/data/sample-result.json`. Label the fallback explicitly as a
stored sample and show the release date in `sample-result.meta.json`; never
stamp it as the active release. Use the metadata's generation date in Vietnam
for the fallback status, never today's date. Project the stored sample's first
primary hit to the same closed schema and body cap before sending any data to
the browser. The checked-in excerpt is the reviewed fallback, not a second live
search. If the stored projection fails validation, hide the sample card. Keep
the multi-hit source JSON out of the client bundle, serialized hydration data
and public assets; only the validated projection can leave Nitro. The page's
existing cache and headers remain in force.

Enable the server route before switching the web page to it; an old server or a
disabled route simply leaves the stored sample visible. No corpus, recipe,
database schema or credentials change. Replace the
[release restore runbook's manual refresh](../runbooks/release-restore.md#refresh-the-home-sample)
when the feature ships: after activation or rollback verify that a live sample
names the active release and that the home page uses it or clearly labels the
stored fallback. Do not mint an MCP token, regenerate fixtures or open a web
pull request for each release. Investigate a persistent fallback without
blocking or repeating corpus activation.

## Acceptance

Run the adversarial pass before merge for both an anonymous visitor and a
compromised container calling the API over the host network. Check that direct
calls and spoofed headers cannot select evidence or bypass the common gate, that
no new listener or privilege is needed, and that saturated, cancelled and
malformed requests cannot cause more work than the admission allows. Prove
credential equivalence, one attempt across concurrent cold requests, warm hits,
abstention and embedder failure, activation and rollback while a flight is
running, and unchanged signed-in search and reader capacity under the
[public overload gates](web-public-documents-serving.md#capacity-acceptance). A
long lead cut with a complete named body, a cut only in a descendant and an
unproven cut each fall back. A proven cut inside the named body succeeds. Feed
Nitro hostile 200 responses with extra hits, a wrong document key, extra nested
fields, `hit.text`, and oversized decoded bodies, including an unknown length.
Each must fall back before the page gets the data. Inspect the API body,
rendered page, client assets and decoded hydration data for markers planted in
forbidden fields and a nonselected hit on both paths. Check the live status date
across Vietnam midnight and the fallback after an outage and a release mismatch.

Before the first production enablement, run the fixed query once against a real
release and confirm a 200 that Nitro renders as the live sample. If the fixed
query only falls back, choose another fixed query before enabling. This is a
one-time feature acceptance, not a gate on later releases.
