# Control frameworks: licensed parsers

Parse only hash-verified publisher inputs. Fail the step when a parser cannot
account for every control or its lineage. No model writes canonical text. See
[build](frameworks-build.md) for the artifact contract and
[measurements](../research/frameworks-parsers-2026-11.md) for registry count
proposals. All versions here remain `licensed`.

## Inputs and extraction

The build currently passes one `catalog` file to `parse.Parse`
([build/parse.go:77–95](../../internal/frameworks/build/parse.go#L77-L95)). Most
of these editions register
[`text` PDFs](../../sources/frameworks.yaml#L308-L313); CSA CCM has
[two `catalog` workbooks](../../sources/frameworks.yaml#L508-L518).

Require `parse_source` for each new parser. It must exactly equal a
`files[].path` of the same version, with `text`/`pdf` or `catalog`/`xlsx` as
required by that parser. One selector checks membership, role and format for
build, `frameworks <code> <version>`, `frameworks edges` and the titles
commands. Legacy parsers without `parse_source` use the same selector's unique
catalog rule. Refuse missing or mismatched selections without falling back to
another file. Build reads only verified snapshot bytes; local commands use the
registry reader. Edges use the selected control catalogs and keep separate
mapping workbook selection for mapping inputs.

The diagnostic and edges commands currently pick catalogs independently
([command/command.go:37–81](../../internal/frameworks/command/command.go#L37-L81)).
For a licensed edition, a successful diagnostic prints only the approved
per-version total. On failure, print no partial output and return only a generic
error. `frameworks edges` is silent on success when it processes any licensed
inputs, and uses the same generic failure rule. If access cannot be determined,
fail with the generic error. Free-only diagnostics retain their existing
details.

### Source row provenance

`text_source` is the representation assigned to a source row, not proof that it
supplied text. Use `pdf_text` for text-layer PDFs and `structured` for
structured catalogs and mapping workbooks. `ocr` belongs to the scan path.
`text_complete` is true only for the selected source after the parser captures
all text in the edition's profile. Intentional exclusions do not make it
incomplete. A failed selected parse fails the step; do not write an incomplete
selected row and continue. Unselected source rows, including CAIQ and mapping
workbooks, keep their representation and have `text_complete: false`, even when
used for edges. Guides and change logs remain outside `source_file`. Bind this
rule to the build recipe. The current loader records `structured, true` for
every row it inserts
([build/database_sources.go:21–32](../../internal/frameworks/build/database_sources.go#L21-L32)).

Use
[Poppler `pdftotext`](https://gitlab.freedesktop.org/poppler/poppler/-/blob/master/utils/pdftotext.1)
26.11.0 on the MacBook, as a separate executable, following the law path's
version and sandbox contract in
[PDF text](pdf-text.md#extractor-and-its-boundary) and its
[process budgets](budgets.md#pipeline). All PDF measurements and parser PR
checks use the same pinned sandboxed adapter as build. The adapter invokes
Poppler directly with `LC_ALL=C`, a minimal allowlisted environment and no
inherited credentials:

```text
pdftotext -layout -enc UTF-8 -eol unix - -
pdftotext -bbox-layout -enc UTF-8 -eol unix - -
```

The first form preserves the page and row structure of prose. Split on form
feeds in physical page order. Use the second form for PCI and COBIT practice
cells, where word boxes separate columns. COBIT runs both forms sequentially on
the same verified bytes, with the first for objective fields. Ordinary reading
order loses cell boundaries. A linked Go/C PDF library would duplicate the
runtime boundary without a measured advantage. XLSX checks use the workbook
parser.

Decode word-box XML without external entities or network access. Reject invalid
UTF-8 and any literal or numeric character reference outside the
[XML 1.0 character range](https://www.w3.org/TR/xml/#charsets). The decoder
accepts an explicit per-call option naming one invalid code point to replace
with U+FFFD in word text and returns the number of replacements. Other invalid
characters and occurrences outside word text still fail. A parser may pass the
option only when the verified source SHA-256 matches its pinned fingerprint. The
parser must check the returned count against its profile before accepting the
result. Keep the option and replacement count internal to extraction.

Feed only the hash-verified bytes to stdin; never pass or reopen the original
path. Bound stdout in memory. Deny network access and filesystem reads outside
tool runtime files. Enforce the law input, memory, scratch, stdout, stderr,
concurrency and time limits; kill and reap the process tree on cancellation.
Refuse to run if the version pin, sandbox or resource limits cannot be enforced.
On macOS the sandbox is a `sandbox-exec` deny-default profile that allows only
the tool's runtime files, `/dev/null` and the inherited pipes, so the scratch
limit is zero writes. macOS enforces neither `RLIMIT_RSS` (a reclaim preference,
`man 2 setrlimit`) nor `RLIMIT_AS`, so the memory limit is a watchdog: it
samples the resident memory of the subprocess every 50 ms and kills its process
group above the limit, which fails as an exceeded limit. One sample may take up
to 1 s on a loaded machine; a failed sample, or no completed sample for 1 s,
fails the run as an unenforceable limit. The version check runs under the same
60 s per-call bound as extraction. The sampling gap is accepted because every
input is a registry-pinned file. Never log extractor output or raw errors.
Discard bounded stderr on success. Nonzero exit, exceeded limit, invalid output,
a missing target region or unusable text in a required body region fails
`parse`. Empty non-content pages do not fail. Do not fall back to OCR on
extraction or parser failure; use separate scan eligibility in
[scans](frameworks.md#scans).

The parse recipe binds the exact extractor version and flags, parser revisions
and normalization rules. Today it records only the output schema
([build/run.go:75–104](../../internal/frameworks/build/run.go#L75-L104)). A
recipe or input change invalidates reuse under the existing resume rule. Same
snapshot and recipe must give byte-identical records. This adds no serving
runtime, external service or per-document fee. Run PDF work only locally.

## Controls and guards

A parser classifies a line or block by its document region before testing the
citation grammar. Skip contents, running furniture, sidebars and noncanonical
repetitions before opening a node. Use only pinned layout profiles checked
against the registered source, never an unbounded search for text that resembles
a citation. If a required region, anchor, row or continuation is ambiguous, fail
rather than emitting a partial catalog. A count match alone cannot prove that a
body was assigned to the right control.

Recognise each structural element (table and section headers, footers, captions,
field and margin labels) in one classifier per edition, as its complete printed
label inside its measured band and window. A single word or the presence of
separate words never identifies one. Assign text to a node line by line, and
fail when a line's owner is ambiguous.

Code and tests hold a label only as the edition's profile writes it: in full
where the profile names it, otherwise by its shape (word count, first and last
word, band and window), which the profile then states. They never hold a title,
other publisher wording or a hash of any of these. A hash of short text is as
readable as the text, and the only published hashes are whole-file SHA-256
values ([access](frameworks-access.md#licensed-material-and-where-it-may-be)).

Use printed citation spelling except for the indicated namespace and derived
keys. Use the existing case/space normalization and exact count check
([parse/parse.go:56–74, 103–113](../../internal/frameworks/parse/parse.go#L56-L74)).
Reject missing or nonpositive `expected_controls`, malformed citations,
normalized collisions, missing or wrong parents, cycles and ordinal collisions.
The version profile checks the expected parent before common validation; the
current lineage guard checks indices, ordinals and cycles, not citation rules
([build/parse.go:98–114](../../internal/frameworks/build/parse.go#L98-L114)).

Emit nodes in source order with zero-based ordinals and parent before child. A
root has `Parent = -1`; never promote an orphan. Keep direct text only, not
child text. Join wraps deterministically with spaces and preserve paragraphs,
explicit lists, spelling and punctuation. Do not guess dehyphenation or infer
text from another edition. Keep printed headings only as `title_original`; our
titles come from the import below. Group headings can have empty body; fail on
empty required statement cells. Store no extra notes, tables, mappings or
relations unless specified below. Use `active` and null `supersedes` and
`amends` on these controls except the amendment. Parent position is tree
lineage, not a cross-version link.

## Version profiles and release order

One parser ID per edition. Share the extraction adapter and the heading/body
state machine across ISO editions, not selection ranges or citation grammars.
Land one parser per PR: CSA first (structured rows and complete seed), then PDF
foundation with ISO 27002, ISO 27017, AICPA, COBIT, ISO 27001 base, ISO 27001
amendment, ISO 27018, TCVN 11930, and PCI last (column geometry). The titles
import is an independent PR and must precede a licensed release.

See [parser profiles](frameworks-parser-profiles.md) for the edition rules.

## Titles import and coverage

Add offline `corpus frameworks import-titles --input <file>` and
`corpus frameworks check-titles`. Use the legacy CSV header
`framework_code,version_label,citation_norm,title`. Locally stage a reviewed
copy of the rows for the parser-enabled editions, with missing paraphrases added
and stale rows adjudicated. Never infer missing titles from publisher headings.
A title author checks the private edition and writes our paraphrase; review it
as a public field before import. Do not alter the legacy seed.

Run the same hash-checking reader, parsers and lineage checks as build, but stop
at the citation catalogs and do not require a database, embeddings or an
existing title file. Join exact registry `code` and `version`, then match the
legacy normalized citation to the parsed citation. Refuse unknown code, version,
non-enabled edition, unknown citation, duplicate normalized key, bad CSV or
empty title. Never silently skip a row.

Stage `code,version,citation,title` with exact display citations from the
catalog, sorted by code, version and normalized citation. Use mode `0600` in the
checked private directory. Before renaming the staged CSV, run the same coverage
check as `check-titles`: each licensed control, including roots and derived
keys, has exactly one nonblank title, and no row in any edition is unmatched.
Only then atomically replace `FRAMEWORKS_SOURCE_DIR/trachuan/titles.csv`. Leave
the old file untouched on any failure. Do not echo input values or titles.

Build uses the same key validator, not the import coverage gate. Unknown rows
are errors; missing licensed titles, including an absent CSV in the snapshot,
keep the existing `title_missing` signing gate. Free editions can still fall
back to their publisher title. The present reader checks only empty or duplicate
exact keys
([build/titles.go:29–44](../../internal/frameworks/build/titles.go#L29-L44));
build joins display citations and records missing-title gaps
([build/parse.go:52–62](../../internal/frameworks/build/parse.go#L52-L62)). Run
coverage against each actual source edition locally when enabling it and before
signing, never in CI against private files.

## Acceptance tests

Use only invented, redistributable PDFs, workbook grids and CSVs. Each parser
has a synthetic example of its region transitions, wraps and parent rules, plus
count mismatch, duplicate, wrong/missing parent and malformed layout cases.
Include the relevant traps: header/footer collisions, continued rows, wrong
worksheet or file, neighboring table cells, aliasing false references, glossary
bullets, amendment self-links, and duplicated printed IDs outside the exact TCVN
profile. Test that the word-box replacement option requires a matching source
hash and replacement count. Check rejection of invalid literals and numeric
references, including another invalid code point with the option set. Test
output determinism and that extraction failures and malformed rows never expose
a sentinel publisher string in errors or diagnostics. Test selected and
unselected source rows and reject divergent source selection across entry
points. Test poor title coverage, stale rows, normalized duplicates and failed
import leaving the old CSV intact using an invented `FRAMEWORKS_SOURCE_DIR`.

### Private acceptance before enabling a parser

The [measured totals](../research/frameworks-parsers-2026-11.md) are
provisional. Each parser PR must verify the edition's inventory against the
registered source through the same reader and adapter as build. Check the
citation openings, order and parentage. For AICPA, confirm the table region,
bullet nesting level and owning criterion for each point of focus. For TCVN,
verify both duplicate pairs satisfy the
[ordered neighbor rule](frameworks-parser-profiles-tables.md#tcvn-119302017-tcvn11930-2017-pdf).
Only after this check put the verified positive total in `expected_controls` in
that parser PR. Never derive the expected total during a build.

Privately compare the parsed records against the rendered PDF or worksheet.
Check where every body begins and ends, what crosses a page or row boundary, and
that excluded columns and sections did not enter the body. Before enabling the
parser, save a regression baseline under `FRAMEWORKS_WORK_DIR`, keyed by edition
and source hash. Store only counts and hashes: total, source and recipe hashes,
and SHA-256 of the reviewed canonical parser records before the title join, both
as a whole and per record in canonical order. Later parser changes, including a
change to the Poppler pin or the adapter, must compare against this baseline,
review each change against the source, and replace the baseline only after that
review. These are local PR checks, not additional runtime build inputs.

No source text, original titles, or private baseline data leave the
[approved places](frameworks-access.md#licensed-material-and-where-it-may-be).
Only the approved per-version total leaves private PR validation.
