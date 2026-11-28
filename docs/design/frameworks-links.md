# Framework links

Framework links describe source assertions between controls and reviewed links
from controls to legal provisions.
[ADR 0016](../adr/0016-control-frameworks-corpus.md) owns access.
[ADR 0017](../adr/0017-findings-and-reviewed-knowledge.md) allows the
control-to-law claim with law-only evidence. This note extends the mapping rows,
`relate` step and mapping results for the first signed frameworks release.

## Decisions for approval

- Keep all three kinds in `control_mapping`. A typed target keeps one order,
  count and access rule for `mappings` and `read`. Separate tables would give
  the law target a simpler row shape and require merging and paging across
  separate query paths.
- Start law links with the reviewed `related` claim and bind them to a signed
  law release. The fixed relation permits navigation and avoids an assertion
  about legal effect or equivalence. A live law join would let a separate law
  activation change a pinned framework result.
- Approve the public edge facts and admission rule below for licensed sources.
  The approval is for the identity-level assertion. Publisher text and evidence
  remain subject to the existing access rules.

## Kinds and identity

| `kind`           | Ends                                                                          | `relation`                                                  | Meaning                                                           |
| ---------------- | ----------------------------------------------------------------------------- | ----------------------------------------------------------- | ----------------------------------------------------------------- |
| `within_version` | Controls of the same edition                                                  | `related`, `moved-to`, `incorporated-into`                  | Catalog cross-reference or directed withdrawal target.            |
| `cross_version`  | Controls of different editions, including different editions of one framework | `equivalent`, `subset`, `superset`, `intersects`, `related` | The named source's mapping in the stored direction.               |
| `control_law`    | Framework control to an exact legal provision                                 | `related`                                                   | A reviewer connected the control identity to the cited provision. |

A control key is `(code, version, citation)`. A law key is
`(country, key, anchor)` using the law document key and canonical provision
anchor. Version and control `supersedes` and `amends` remain lineage. A
withdrawal link keeps the publisher's direction and verb and adds no lineage or
status change. Preserve both directions if the source supplied both. Generate no
inverse or transitive edges. A review carries no assertion of compliance, repeal
or commencement.

The unique key and order are `(from, target type, to, relation, source)`, with
each identity expanded to its fields, bytewise, and target type `control` before
`law_provision`. `kind` follows from the typed endpoints. The source is a short
registered mapping source name. A reviewed law edge uses `reviewed-knowledge`;
the signed law edition and destination give its evidence locator. Release-local
row ids and evidence have no part in the key or order.

## Public edge inputs

Each source has a closed rule for deriving `(from, to, relation, source)`. The
code and version come from the registered source profile or an explicit
reference. Resolve direct inputs through public registry keys and citations. The
two direct catalogs also supply a fixed relation code. Any direct source must be
the registered file and have approved public terms. When that approval lapses,
use the reviewed-key input for that source or omit its edges.

| `source`               | Key fields or reviewed input                                                                                                                                                                       |
| ---------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `nist-oscal`           | Catalog control id and citation label, local fragment target, and `rel` of `related`, `moved-to` or `incorporated-into`.                                                                           |
| `nist-csf-withdrawals` | Catalog category or subcategory citation and the target citations and action in the public withdrawal field. Map the action to `moved-to` or `incorporated-into`.                                  |
| `nist-csf-references`  | Catalog function, category or subcategory citation and the public reference column's framework name, version if named, and citation. Use `related`.                                                |
| `nist-olir`            | The registered workbook's `Focal Document Element` and `Reference Document Element` citation columns. Use `related`. Keep the comments column separate as evidence.                                |
| `cis-v8.1-mappings`    | Reviewed-key input only. A join against the publisher's title or safeguard description may suggest a row for offline review. Its output, misses and ambiguity counts stay out of the public build. |
| `reviewed-knowledge`   | Validated `control_law_link` entries, with the law and control identities specified below.                                                                                                         |

New sources need an explicit admission rule. For control-to-control sources that
require review, the owner approves canonical `{from, to, relation, source}` rows
in `FRAMEWORKS_SOURCE_DIR/trachuan/mapping-edges.jsonl`. Treat an absent file as
an empty reviewed set. The snapshot freezes and hashes that file or records its
absence. Accept only sources the admission rule allows to use these rows. Refuse
extra fields, duplicate keys and noncanonical identities. The file carries
approved identity facts only.

Use the direct readers, the frozen reviewed-key file and the validated law
entries to compute the expected public key set and each key's admission basis.
Match that set exactly against the edge artifact on writing and on reuse, using
the frozen inputs. Reject extra or missing keys and a mismatched admission
basis. Apply the same check at load and compare the restored key set during
evaluation. Derive resolution, public gaps and counts only after admission. Keep
raw parser rejects and licence-dependent evidence losses in private build
evidence. No raw title, description or comments value selects a public key.
[NIST OSCAL Rev. 5 catalog](https://raw.githubusercontent.com/usnistgov/oscal-content/v1.4.0/nist.gov/SP800-53/rev5/json/NIST_SP-800-53_rev5_catalog.json),
[NIST CSF 2.0 and reference tool](https://csrc.nist.gov/pubs/cswp/29/the-nist-cybersecurity-framework-csf-20/final),
[NIST OLIR](https://csrc.nist.gov/Projects/olir),
[CIS Controls v8.1](https://www.cisecurity.org/controls/v8-1)

## Relate and schema version 1

Resolve a same-edition target in the exact edition and preserve the source's
relation. The cross-edition version and alias rules remain those of
[build](frameworks-build.md#steps). Resolve canonical citations from parsed
control identities. Keep a valid named framework endpoint when the catalog or
citation is absent, with `resolved: false` and the existing unresolved gap. Drop
malformed endpoints and self-links under the mapping gap rules. After
normalization, collapse identical direct source rows with the same key. Fail on
conflicting payloads under one key. A malformed law review fails validation. A
stale law review produces no edge.

Bump the mapping artifact to `edges/2` and the affected recipes. Store `kind`,
typed endpoint keys, `relation`, `source`, `resolved`, `admission` and optional
`evidence` with `requires[]`. `admission` is `direct`, `reviewed_key` or
`reviewed_law`. Validate it against the independently derived key set and keep
it in the build artifact only. Verify order, resolution and evidence
classification when loading a saved artifact.

In `control_mapping`, add `kind` and the nullable `law_country`,
`law_document_key` and `law_anchor`. A control target requires all three
existing `to_*` business-key fields and null law fields. A law target requires
all three law fields and null control-target fields and ids. Keep the existing
control foreign keys. For a control target, `resolved` is true exactly when both
endpoint ids exist. For a law target, `resolved` is true and `from_version_id`
is required. Check the kind, endpoint shape, version equality or difference,
self-link, relation set and key forms in the schema. Enforce uniqueness with one
partial unique index per target type, and index incoming control edges and the
ordered source/version-pair lookup. Use the bytewise key for pagination.
[PostgreSQL partial indexes](https://www.postgresql.org/docs/current/indexes-partial.html)

`mapping_law_basis` holds `(country, rel)` keyed by country. The law country on
an edge references this row. There is one verified law release per country in
one frameworks build and initially only `vn`. Law document and anchor resolution
is verified during build against that release. The framework serving process
reads the recorded basis and the snapshot's edge facts without querying a law
database.

Continue to store evidence in `mapping_evidence`, after access classes are
sealed. Classify the evidence's source as well as both framework endpoints. For
framework-to-framework evidence the dependency set includes every licensed
endpoint and every additional licensed version the source depends on. A source
with unknown evidence dependencies contributes only approved identity fields.
Refuse any evidence row whose dependencies cannot be verified. Use a canonical
dependency set and verify it when loading or restoring rows. A `control_law`
edge has one evidence row, with an empty dependency set and the exact verified
law quote. This exception applies only to the typed law evidence rule below.

## Reviewed control-to-law entries

Add `control_law_link` to the closed `sources/<cc>-knowledge.yaml` contract.
Reuse the [knowledge entry](knowledge-entries.md) rules for ids, file bounds,
quote lengths, review metadata and batch sampling. `target` holds the law `key`
and `anchor`. `claim` holds `control: {code, version, citation}` and
`relation_kind: related`. `evidence` holds `quote`. `depends` holds `contract`,
`target` with the existing law provision dependency block, `target_own_sha256`
over the exact reconstructed own-text bytes, and `control_sha256` with a hash of
the approved control record. Hash the canonical identity, kind, parent key,
path, ordinal, approved title, status and lineage. Use the existing knowledge
hash format and the canonical JSON encoding of
[release changes](release-changes-encoding.md#lines). The input excludes
publisher text, private fingerprints and incident edges. Changing the encoding
requires a contract version change. The entry id records review history. The
public edge identity remains stable if that id changes.

The reviewer approves a public association using the public control record and
the anchored law text. Use no control quote, paraphrase, private digest or free
text rationale in the entry. Refer a claim that requires private control facts
back for review. A change in the public association requires a reviewed input
change. Changing only private text never creates, removes or reclassifies an
edge. Exact dependence on the law provision uses the existing rules for its
text, heading and ancestors. Once dependencies match, require the quote to be an
exact substring of the target's own text. Use the same own-text projection as
the law reader's per-provision range units. For an article with embedded clause
text, validate against the article's own paragraphs after separating clause
text. A quote present only in a child is invalid for the parent.
[Law text structure](text-structure.md#what-is-recorded)

The Git check and both build paths use the shared knowledge validator for
structure. The review check and framework build additionally validate against
both frozen inputs. For each named law country, verify the law manifest and its
dump digest, size and restored release identity. Extract from that exact restore
in one read-only snapshot. Freeze the knowledge file, the manifest's release and
digest, and the following data for each named document and anchor:

- Document presence, `number_norm`, `text_source`, `text_complete`, the
  `baseline_served` gap and the facts needed for law reader eligibility.
- Target presence, stored text and heading, and the ordered ancestor chain with
  each anchor and heading. Read the named document's complete anchor tree to
  mark, at every target and ancestor level, a `~k` suffix or a sibling with one
  under the same parent. A plain `dieu-5` with `dieu-5~2` is ambiguous.
- Target subtree text and ordering needed to reconstruct the anchored
  provision's own text. Freeze the resulting own text and ambiguity flag from
  the same read projection. An ambiguous reconstruction cannot supply evidence.

`relate` reads those inputs, the current run's `parse` artifact and the
snapshot's approved titles. A missing or mismatched selected law release fails
the run. An absent control, absent or unavailable provision, ambiguous anchor or
reconstruction, or changed dependency makes the entry stale. An invalid quote
with matching inputs, a duplicate claim or an unknown field fails validation.

Record validation outcomes in `knowledge-report.json` using the existing
knowledge report shape. For a stale entry whose framework version is present,
add a public, nonblocking `knowledge_stale` gap. Its subject is the entry id.
Its detail is `1` for an absent control, `2` for unavailable law, `3` for an
ambiguous anchor or own-text reconstruction and `4` for a dependency mismatch.
Use this order when several apply. If the version is absent, record the stale
entry only in the report. The law build's validity, timeline and findings rules
remain law-only.

Start with TCVN 11930:2017 against Circular 09/2020/TT-NHNN and Decree
85/2016/NĐ-CP. Resolve exact corpus keys and anchors during review. Then cover
NIST SP 800-53 and CIS Controls against ISO/IEC 27001:2022 and PCI DSS 4.0.1
through the cross-version path, preserving admitted source claims.
[Standards catalog](https://tieuchuan.vsqi.gov.vn/tieuchuan/view?sohieu=TCVN+11930%3A2017),
[official circular](https://congbao.chinhphu.vn/noi-dung-van-ban-so-09-2020-tt-nhnn-32321?cbid=33063),
[official decree](https://vanban.chinhphu.vn/?pageid=27160&docid=185601)

## Serving

`mappings` and framework `read` return the same edge shape and order. Add `kind`
to the edge. Keep the existing reduced shape of each control end for every
caller, including the unresolved key form. A law `to` has `country`, `key`,
`anchor` and `release` from `mapping_law_basis`. The caller can form the law
`<key>#<anchor>` handle and request it in a separate law `read` with that
release. The law reader enforces its own access, eligibility and release pin. If
the pin expires, the existing law error applies. The framework response still
describes the reviewed snapshot.

A `handle` query takes one control handle and returns incoming and outgoing
edges. `from` and `to` take two framework version handles, including equal
handles for within-version edges. Law links appear under the source control's
`handle` query and its `read` mapping units. Version `read` stays head and
outline. `relation[]` accepts the union in the kind table. A law handle or a
mixed batch follows the existing invalid-input rules. Framework search and
status counts include these admitted edges. Each edge counts once at each
control end and once at its source version for status.

Use the existing [access decision](frameworks-access.md#decision), rollout,
request bounds and evidence split rule. Select page keys, counts and cursors
from the public edge rows. A separate evidence query selects only values whose
whole dependency set is readable. `mappings` caps permitted evidence and `read`
emits it in split units under the existing limits. A withheld value has exactly
the same output, unit boundary and continuation as absent evidence. Bind the
`mappings` cursor to selector, relation filter, limit, release, access set and
typed last key. The existing `read` continuation bindings apply.

## Proof and release order

Use invented standards and provisions. Check all three kinds from input through
dump and restore. Exercise duplicate and conflicting input, invalid shapes and
relations, unresolved citations, and stale or ambiguous law review. Hold the
reviewed keys fixed and perturb licensed publisher titles through snapshot,
relate, load, and restore. Require identical keys, order, public counts, gaps,
and downstream artifacts. Inject an extra key, remove a key and change an
admission marker in saved artifacts. Each must fail validation.

Reuse the invented CSF fixtures `TestCSF_RejectsInvalidMappings` and
`TestCSF_DescribedFunctionReferences` from
`internal/frameworks/parse/review_fixes_test.go`. Carry the category withdrawal
and the function reference through `relate`, dump restore and reduced export.
Check the withdrawal's exact target and `moved-to` relation. Check that only the
reference from the described function row survives in both row orders. Register
an invented target framework for the function reference in the test.

Check stale review for a `baseline_served` or incomplete document, an ancestor
heading change, and `dieu-5` with duplicate `dieu-5~2` at the target or ancestor
level. A quote from clause 2 alone fails when anchored to its article and passes
at the exact clause. Reject an ambiguous own-text reconstruction. Exercise both
selectors, reverse incidence, read units and paging on MCP and web. Run the
[serving invariant](frameworks-results-invariant.md) with mutated licensed text
and evidence, mixed access and a law release swap. Measure edge and index bytes
and read latency before signing.

Rebuild unsigned runs under a new `rel`. Frameworks schema stays version 1 until
its first signature. Follow the [release order](frameworks-release.md#order) for
the compatible image, release floor and first upload.
