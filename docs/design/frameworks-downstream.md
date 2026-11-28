# Downstream framework releases

Give sc365 a reduced snapshot and one change list per signed frameworks release.
Use the keys from [framework links](frameworks-links.md#kinds-and-identity) in
private asset mappings. The [access note](frameworks-access.md) owns which
fields may cross the boundary. A signed release dump stays on the frameworks
release path.

## Decisions for approval

- Sign the identity and changes file digests in the frameworks release manifest.
  sc365 can then verify both with the existing release signature. An unsigned
  export checksum would detect damage and allow an attacker with write access to
  substitute both data and checksum.
- Use an offline bundle of JSON Lines for the first sc365 import. The two files
  can be streamed and joined by stable keys. A network endpoint would add
  hosting, paging, access and retention work to the same import.
- Approve the formats and sc365 import behavior after its trial with a baseline
  and a comparison. Approval to ship our titles in a sold product comes from the
  licence terms review. Keep `terms: pending` until that review records a
  decision.

## One reduced projection

Build one closed projection for `identity.jsonl` and derive `changes.jsonl` from
two instances of that projection. Build and evaluate use the same field rules.
The command packages the verified result. It has no full export mode or
caller-supplied field list.

Select the listed columns and construct typed objects for every level. Check
frozen version classifications and evidence dependencies at source. Read
`control_text` only through the `free` version selection. Read edge evidence
only through the public evidence selection. A licensed endpoint or evidence
source cannot contribute any text except the separately verified law quote on a
`control_law` edge. Unknown access, record shape, evidence class or unlisted
nested field fails generation or validation before publication. Never copy
general DTOs, database rows, raw reports or step manifests into the bundle.

### `identity.jsonl`

The first line is `{kind: "header", version: 1, rel, law_basis}`. `law_basis` is
an object from country code to verified law `rel` for the included reviewed
edges, or `{}`. Subsequent records have these fields:

| `kind`    | Fields                                                                                                                                                                                                                                                   |
| --------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `version` | `id: {code, version}`, `name`, `publisher`, `language`, `released`, `ordinal`, `access`, `licence_kind`, `licence_url`, `supersedes`, `amends`. Licence URL and lineage keys may be null.                                                                |
| `control` | `id: {code, version, citation}`, `control_kind`, `parent`, `ordinal`, `status`, `title: {value, terms}`, `supersedes`, `amends`. `parent` and lineage are control keys or null. A `free` control also has `text: {title_original, body, notes, tables}`. |
| `edge`    | `edge_kind`, `from`, `to`, `relation`, `source`, `resolved` and optional public `evidence`. `from` is a control key. `to` is a control key or `{country, key, anchor}`, as `edge_kind` requires.                                                         |

`released` is a `YYYY-MM-DD` date or null. Map the database's `-infinity` to
null, including after restore. Refuse any other non-date representation.
[PostgreSQL special date values](https://www.postgresql.org/docs/current/datatype-datetime.html#DATATYPE-DATETIME-SPECIAL-TABLE)

Version metadata comes from the approved registry fields and lineage. Control
fields come from the approved reduced record. `title.value` is trachuan's
approved display title, possibly empty, and every `title.terms` is `pending` in
version 1. Internal displays may use it. sc365 excludes it from sold products
while pending, including its public display, exports and bundled search data.
The free source's `text.title_original` is separate and travels with its
registry licence. Preserve complete, ordered free text fields as stored. A
licensed record omits `text` entirely.

Use the public edge key and source name from
[framework links](frameworks-links.md#kinds-and-identity). The regular evidence
selection requires the proven empty licensed dependency set. The verified law
quote uses the separate law rule, including when its source control is licensed.
Other edges omit `evidence` when no exportable value exists. The file has no
source-file metadata or hashes, private review provenance, withholding
indicators, diagnostics, index fields, text lengths, publisher text of licensed
editions or release-local ids.

### `changes.jsonl`

The header is `{kind: "header", version: 1, rel, previous, baseline}`.
`previous` is null for the first release and otherwise `{rel, identity_sha256}`
from its verified predecessor. The first release has `baseline: true` and zero
change records. A later empty comparison has `baseline: false` and zero change
records. Use `identity.jsonl` to seed the first import. Every subsequent release
compares with its selected predecessor, including after a rollback.

| `kind`                                                                           | Fields and trigger                                                                                                                                   |
| -------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------- |
| `version_added`, `version_removed`                                               | `id`, when an edition key arrives or leaves.                                                                                                         |
| `version_changed`                                                                | `id`, `fields` for changed approved version metadata. An access change also carries `access: {before, after}`.                                       |
| `control_added`, `control_removed`                                               | `id`, when a control key arrives or leaves.                                                                                                          |
| `control_changed`                                                                | `id`, `fields` for changed `control_kind`, `parent`, `ordinal`, `status`, `title`, or `text`. Compare `text` only if both versions are `free`.       |
| `version_superseded`, `version_amended`, `control_superseded`, `control_amended` | `successor`, `predecessor`, `change: added` or `removed`, one record per changed explicit lineage link. An edited link is a removal and an addition. |
| `edge_added`, `edge_removed`                                                     | `edge_kind`, `from`, `to`, `relation`, `source`, when an edge key arrives or leaves.                                                                 |
| `edge_changed`                                                                   | The same edge key fields and `fields`, from `resolved` and `evidence`, when those public values change.                                              |

`version_changed.fields` can name `name`, `publisher`, `language`, `released`,
`ordinal`, `access`, `licence_kind` or `licence_url`. Every `fields` value is a
nonempty, sorted set of field names. A citation rename yields a removal and an
addition unless explicit lineage also links the identities. A changed relation
or edge source changes its key. Revoking free access emits the version access
change and strips `text` from the new projection. The comparison carries no
old/new text, digest, edge evidence or inferred replacement. Changes describe
records in the corpus and carry no conclusion about legal effect.

### Encoding and bounds

Both files use UTF-8 and LF. Write compact JSON in the canonical encoding of
[release change projection encoding](release-changes-encoding.md#lines) and
start with exactly one header. Order identity rows by kind, then by key in byte
order. Order changes by kind, then by the corresponding public key and, for
lineage, by successor, predecessor and change. Expand each key to its fields as
in the links note. Refuse duplicates, unknown fields or kinds, unlisted optional
fields, invalid key forms, and noncanonical or over-budget input before
accepting a file. The proposed bounds are 1 MiB per line and 64 MiB per file,
applied to readers and writers by bytes. Measure the largest held release before
confirming those limits in [budgets](budgets.md).

## Build, signing and command

1. During snapshot, freeze the predecessor that evaluation already compares
   against. Verify the predecessor's signed frameworks manifest, report hash and
   `identity.jsonl` digest and header. Record the exact predecessor identity and
   SHA-256 in the snapshot inputs. Bootstrap uses no predecessor. If a
   predecessor is named and a required file is absent or mismatched, stop the
   build. A retry uses the frozen bytes. Upload still checks that the selected
   predecessor of a new release is the production release.
2. Build writes the identity and change list with the frozen inputs. Bind their
   SHA-256 and the predecessor binding in `build.json`. Evaluate independently
   projects the verified restored dump, regenerates the comparison, and checks
   byte equality and the digests. Write the checked copies next to `report.json`
   in the immutable evaluate step. Its report binds `build.json` and requires
   this check for `quality_pass`.
3. Upload stages the two files beside the existing release artifacts in the
   frameworks release bucket. Upload and signing verify the file contracts,
   their digests against `build.json` and the predecessor binding against the
   verified predecessor manifest. Signing adds
   `downstream: {version: 1, identity_sha256, changes_sha256}` to the closed
   frameworks manifest. Every digest is 64 lower-case hexadecimal digits.
   Re-signing keeps these digests and the original predecessor. Extend the
   compatible verifier and its tests before the first release signs. The law
   manifest stays as specified in
   [law signing](corpus-signing.md#corpus-manifest).
4. Run `corpus frameworks export <rel> --signed-release <dir> --out <dir>` on
   the MacBook. The signed release directory supplies the exact frameworks
   manifest and Sigstore bundle. The checked run directory supplies the two JSON
   Lines files. Validate the signature against the existing trusted frameworks
   workflow identity and issuer, the release and predecessor headers, schemas
   and matching signed digests, before publishing.
   [Sigstore verification](https://docs.sigstore.dev/cosign/verifying/verify/)

`--out` must name a new directory. Write the five exact files to a staging
sibling on the same filesystem, then sync and rename as one unit. Refuse
symlinks, unsafe inputs and partial delivery. Honor the build's checked work
paths for inputs. Allow only approved projection bytes and the signed release
manifest with its signature into staging. On failure clean up staging and report
the failed check without record values. Verification may use the existing
signature verifier's trust material. Export reads no source files, corpus dump
or database and publishes no network resource.

| Bundle file                      | Contents                                                                                                                                                                                                                                       |
| -------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `identity.jsonl`                 | Complete snapshot of allowed records.                                                                                                                                                                                                          |
| `changes.jsonl`                  | Comparison for this release.                                                                                                                                                                                                                   |
| `manifest.json`                  | Closed export manifest `{kind: "trachuan.frameworks-export", version: 1, corpus_kind: "frameworks", rel, previous, files}`. `previous` is the change header value. `files` has exactly the two JSON Lines filenames, each to `{sha256, size}`. |
| `release-manifest.json`          | Unchanged signed frameworks manifest. Its `rel`, `previous`, and signed sidecar digests must agree with the export and headers.                                                                                                                |
| `release-manifest.sigstore.json` | Verifier bundle for the release manifest.                                                                                                                                                                                                      |

The export manifest records sizes and digests of the allowed bytes only. The
copied signed release manifest carries the existing integrity exceptions in
[access](frameworks-access.md#licensed-material-and-where-it-may-be). The
Sigstore bundle attests that manifest. Keep the full build manifest, report,
predecessor files and release dump outside the handover. The added sidecar
fields certify only the approved projection.

## sc365 import

Verify the signature and match both data files to the signed digests and the
export manifest before changing local data. Require known versions and kinds.
Before the lock, prepare only data from the verified incoming bundle. Begin the
import transaction and lock the framework import state before reading the
installed snapshot, law basis, private mappings and sequence. Check the incoming
sequence against that locked state. Refuse a sequence below the highest imported
sequence. At that sequence, only an identical signed manifest is an idempotent
success. Compare the SHA-256 of its exact manifest bytes, regardless of the
signature envelope. Refuse the same `rel` with different data digests, including
at a higher sequence. Store the highest sequence and manifest digest in the same
transaction as the new snapshot, review records and cache invalidations. A
failed import advances neither value.

A higher sequence for the installed `rel` with matching data digests updates the
watermark without duplicating review records. A higher sequence for an older
`rel` is a [re-signed rollback](frameworks-release.md#signing-workflow). A
skipped release or rollback requires the full identity snapshot and
reconciliation from sc365's locked installed state. Apply stepwise change
records only when the installed `rel` and identity digest match `previous` and
the import moves forward. Compute every change, review item and cache
invalidation from that locked state inside the same transaction. Retain private
asset links by key and queue missing keys, changed fields, incident edge changes
and lineage for review. Migrate an asset link only after review.

Replace sc365's `law_basis` from the verified identity header on every import.
Compare it with the installed basis, treating a missing country as removal.
Refresh all law references for each changed country in that transaction, even
when `changes.jsonl` has zero records.

For an edition that ceases to be free, remove formerly free text from the
current sc365 view, export and search, and invalidate its caches in that
transaction. If a cache lives outside the database, advance its generation in
the transaction and have readers use the generation of their installed snapshot.
Resolve law links with the law importer using country, key and anchor. The law
basis says which law release verified the quote. A law import with a missing or
changed target prompts a review and grants no authority to silently substitute a
provision. Private asset mappings store keys and local decisions. The imported
snapshot holds the corpus evidence. Record sc365's feedback on a baseline and a
release pair, including a removed key and a free-to-licensed transition, before
freezing either format.

## Proof

- Build a synthetic pair with free and licensed editions, mixed-source mapping
  evidence and one law link. Hold public facts fixed and mutate licensed body,
  notes, tables, original title, private evidence presence, embeddings, source
  metrics and private gaps. Check byte equality of both data files and their
  export manifest. For the signed release manifest, keep release metadata fixed
  and compare all bytes except the existing integrity fields. Verify the
  signature envelope separately. Assert the input queries never select fields
  from licensed text rows or private evidence.
- Inspect every nested object and compare exact emitted keys. Try unknown
  fields, a hidden evidence dependency, a licensed title-based edge join, and
  approved identity-only inputs with identical public keys. Fail closed or
  withhold by the projection rule. Exercise optional values, evidence with no
  readable dependency set, and distinguish the verified law quote from licensed
  text. Include marker strings and mutated lengths and derived values in
  forbidden fields. Test full free text, pending titles and the access flip in
  both files.
- Verify baseline, empty comparison, additions, removals, metadata and free text
  edits, edge resolution/evidence edits, lineage changes, and stable ordering
  despite row-id or input order changes. Exercise retries, failed output writes,
  wrong or missing predecessor, mutated sidecar bytes, unknown format, valid
  signature for a different release and a forged export manifest. Repeat on the
  restored dump and verify the signed round trip.
- Use signed bundles in the sc365 trial. Import a release with free text, then a
  higher sequence that reclassifies the edition as licensed. Replay the first
  bundle and verify refusal, unchanged import state and continued absence of the
  old text. Verify the identical highest manifest is a no-op and a different
  valid manifest at that sequence is refused. Accept an older `rel` signed at a
  new highest sequence and reconcile from the full snapshot. Verify that a
  rejected or failed import leaves the sequence, snapshot, review queue and
  cache generation unchanged.
- Prepare two imports while R0 is installed. R1 adds a control and R2 removes
  it. Let R1 commit first. R2 must compute its review queue against R1 under the
  lock and record the removal. Repeat with R1 adding a free edition and R2
  reclassifying it as licensed. Verify that R2 removes its text and invalidates
  caches after R1 commits.
- Import a comparison with zero records and only a `law_basis.vn` change. Check
  that sc365 refreshes every Vietnamese law reference. Cover a removed basis, a
  missed predecessor and a removed control key. Build and restore an edition
  with no release date and assert `released: null` in both projections. Measure
  both file sizes and build, signing and import time. The host needs no bundle
  storage or serving path.
