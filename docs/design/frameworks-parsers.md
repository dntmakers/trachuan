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
[two `catalog` workbooks](../../sources/frameworks.yaml#L508-L518). For each new
parser require `parse_source`, a version field naming exactly one registered
file of the required `role` and `format`: `text`/`pdf` for PDF, `catalog`/`xlsx`
for CSA. Refuse a missing or mismatched selection. Existing parsers keep their
unique catalog selection. Guides, other catalogs, change logs and mapping sheets
never become control text by fallback. Build parses from the snapshot only. Set
`source_file.text_source` to `pdf_text` for the successfully parsed PDF, not the
current `structured` constant
([build/database_sources.go:21–32](../../internal/frameworks/build/database_sources.go#L21-L32)).

Use
[Poppler `pdftotext`](https://gitlab.freedesktop.org/poppler/poppler/-/blob/master/utils/pdftotext.1)
26.11.0 on the MacBook, as a separate executable, following the law path's
version and sandbox contract in
[PDF text](pdf-text.md#extractor-and-its-boundary) and its
[process budgets](budgets.md#pipeline). Run with `LC_ALL=C`, without a shell or
inherited credentials:

```text
pdftotext -layout -enc UTF-8 -eol unix - -
pdftotext -bbox-layout -enc UTF-8 -eol unix - -
```

The first form preserves the page and row structure the prose parsers need;
split on form feeds, retaining physical page order. Use the second for PCI,
where word boxes, not whitespace or reading order, separate cells. Parse the XML
without external entities or network access. Use verified bytes as stdin and
keep stdout in bounded memory. Ordinary reading order (the law default) loses
cell boundaries; a linked Go/C PDF library would duplicate the runtime boundary
without a measured advantage. The workbook parser needs no PDF tool.

Use a context-bound subprocess, with no network or unrelated filesystem access,
with the law input, memory, stdout, stderr, concurrency and time limits, and
kill and reap the process tree on cancellation. Only the verified input and tool
runtime files can be read. Refuse PDF parsing if sandboxing, resource limits or
the version pin cannot be enforced. No extractor output or raw errors go to
logs. Discard bounded stderr on a successful exit; nonzero exit, exceeded limit,
invalid output, a missing target region, or unusable text in a required body
region fails `parse`. Empty non-content pages do not fail. Do not fall back to
OCR on a tool or parser failure. Use separate scan eligibility in
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

Use printed citation spelling except for the indicated namespace and derived
keys. Use the existing case/space normalization and exact count check
([parse/parse.go:56–74, 103–113](../../internal/frameworks/parse/parse.go#L56-L74)).
Set positive `expected_controls` from reviewed measurements in the parser PR,
not from a count learned on each run. Reject missing counts, malformed
citations, normalized collisions, missing or wrong parents, cycles and ordinal
collisions. The version profile checks the expected parent before common
validation; the current lineage guard checks indices, ordinals and cycles, not
citation rules
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

### CSA CCM v4.1: `csa-ccm-4-1-xlsx`

Select the CCM workbook, not CAIQ. Use the exact `CCM` worksheet with
[`rows`](../../internal/frameworks/parse/xlsx.go#L11-L26), checking row 3's A–D
headers and positions before reading records. A domain header in A with no ID in
C opens a `domain` root keyed by its terminal uppercase acronym, including `&`.
A row with an ID in C is a `control` (`<acronym>-NN`), parented to the active
domain with that acronym. Keep the title in B and specification in D; retain the
labeled applicability values in E–W as paragraphs after the specification, in
column order. Reject malformed IDs, missing title or specification, a mismatched
domain or an unexpected row inside the table. Skip preamble, blank and trailer
rows and do not parse other sheets.

### ISO/IEC 27002:2022: `iso27002-2022-pdf`

Start at the first thematic heading in the body, not its contents entry. Stop
before annexes. The flush-left `5`–`8` headings give `domain` roots; `N.N`
headings give `control` children. Keep control statements, narrative guidance
and references in their own section, not the separate attribute/tag matrix or
annex tables. A dotted citation in a paragraph is not a heading.

### ISO/IEC 27017:2015: `iso27017-2015-pdf`

Start at the first numbered control-guidance section after front matter and stop
before bibliography/back matter. Reject page-footer numbers before heading
detection. Keep numbered sections `5`–`18` and their printed subsections as
`control` nodes, parented by the immediate dotted prefix. In the cloud annex
also keep `CLD.N.N` roots and their `CLD.N.N.N` children as `control`. Keep
direct narrative guidance and explicit reference-only text as printed; never
pull in ISO 27002 text to fill a reference. Omit standalone comparison or
guidance tables.

### AICPA criteria: `aicpa-tsc-2017-pdf`

The 2022 points-of-focus revision is the source for registry version `2017`.
Enter the criteria table, then stop when it ends, before glossary or appendices.
Read `CC`, `A`, `C`, `PI`, or `P` with a numeric group and criterion (for
example `CC1.1`) from the ID column; `P<N>.0` labels groups only. Each
`criterion` is a root and holds its statement up to the first points-of-focus
bullet. Each top-level bullet in that criterion's points-of-focus region becomes
a `point-of-focus` child with
`<criterion>-pof-<01-based sequence padded to 2 digits>`; keep bullet text and
wrapped continuation, with the printed lead-in as the optional original title.
Continue table rows over page breaks; ignore header repeats, nested bullets as
new nodes, and unrelated bullets outside the table.

### COBIT 2019: `cobit-2019-pdf`

Use the objectives volume only. Recognize objective headers in the objective
chapters, then delimit each objective's process component before reading left
cells of its practice table. The `EDM`, `APO`, `BAI`, `DSS`, `MEA` prefixes are
`domain` roots, emitted before their first child. `AAA99` is an `objective`
child of its domain; `AAA99.99` is a `practice` child of the matching objective.
Keep the objective's direct description/purpose fields and each practice's
description cell. Ignore activity lists, metrics, capability levels,
responsibility and information-flow tables, and other components. A practice
reference outside its primary process table never opens a node.

### ISO/IEC 27001:2022: `iso27001-2022-pdf`

Use the base PDF only. Read normative clauses `4`–`10` after front matter and
before Annex A. Flush-left headings open `clause` nodes, split by their printed
dotted prefix. Keep direct clause text; skip page numbers, references in prose
and contents entries. In the Annex A control table, open `domain` roots `A.N`
and `annex-control` children `A.N.N` from the identifier column; add `A.` to the
number printed in the table. Read the adjacent heading and statement cells
across wraps and page breaks. Table labels and bibliography are not text of a
control.

### ISO/IEC 27001:2022 Amd 1: `iso27001-2022-amd1-pdf`

The first ISO 27001 PR splits the amendment file currently registered with the
base ([registry:32–43](../../sources/frameworks.yaml#L32-L43)) into a separate
licensed `iso27001:2022-amd1-2024` entry with `amends: '2022'`. Register the
base parser with its own total; enable the amendment parser with its own total
in its PR. Do not overlay the base. A standalone target citation followed by its
insertion instruction opens an `amendment` root with the same citation in the
amendment edition. Body is the verbatim insertion text, not the editorial
instruction or a rewritten clause. Unknown operations, missing insertion text or
repeated or unresolved targets fail.

Persist version `amends` in registry metadata and resolve it to the same-code
base version on insert, in dependency order without updating sealed versions.
Current inserts omit that link
([build/database_versions.go:32–42](../../internal/frameworks/build/database_versions.go#L32-L42)).
Set the `controls/1` `amends` citation to the base clause. Resolve it against
that explicit version, not the current same-version control lookup
([build/database_controls.go:44–58](../../internal/frameworks/build/database_controls.go#L44-L58)).
Validate the base is present in the release, the target is a base clause, and
there is no self-link, cycle or inferred `supersedes` link.

### ISO/IEC 27018:2019: `iso27018-2019-pdf`

Read the `5`–`18` guidance sections and children as in 27017, without CLD. Keep
every printed numbered heading in the normative body, even if the text only
points to another edition. Enter the data-protection annex separately: its `A.N`
headings open `domain` roots and `A.N.N` headings open `annex-control` children.
Keep direct narrative text; exclude framework comparison tables, introductory
figures and bibliography. Do not synthesize missing parents or replace printed
headings with the prior catalog's selection.

### TCVN 11930:2017: `tcvn11930-2017-pdf`

The registered PDF has a usable text layer. Parse the security-level requirement
sections `5`–`9` and Annex A, not the scope, terms, contents or running headers.
Flush-left headings take the printed number to four components in the main body,
or `A.N`/`A.N.N` in Annex A. A heading with numbered children is a `clause`; a
leaf is a `control`. Use the immediate dotted parent; `A.N` are roots. Preserve
direct Vietnamese text and lists, without translation.

**Approved by the primary (2026-11-22):** the registered file reuses `8.1.3.2`
and `9.1.3.2` for distinct passages. For this exact source fingerprint only,
coalesce each verified pair, retaining both passages in source order (include
the second printed heading in `body`). The first heading supplies
`title_original`. For `N` equal to `8` or `9`, require adjacent `N.1.3.2`
headings between `N.1.3.1` and `N.1.4`. Missing, extra or moved occurrences, or
any other duplicate, fail. Never invent a citation or silently drop either
passage.

### PCI DSS v4.0.1: `pci-dss-4-0-1-pdf`

Read the normative requirement sections `1`–`12` and scoped `A1`–`A3`
appendices. Use their actual section headers as `requirement` roots, not
contents or summaries. On table pages, locate the band from the column headers,
then use the first line of a block in the left cell to detect `N.N` through five
numeric components, or their `A<N>` equivalents. All are `requirement`; parent
is the immediate dotted prefix. Distinguish a repeated continuation header from
a new block and append only its continuation text to the open control. Inline ID
references, overview lists and examples outside the table never open controls.

Keep the left-cell requirements, including the local objective and applicability
text in that cell. Do not mix in testing procedures, right-column guidance or
appendix worksheets. Group and root headings supply title and tree position, not
a copy of their descendant rows. Reject a cell that crosses an unknown column
boundary, a continuation without an open control, or a duplicate opener.

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
empty title. Never silently skip a row. Atomically replace
`FRAMEWORKS_SOURCE_DIR/trachuan/titles.csv` with `code,version,citation,title`,
using exact display citations from the catalog and sorting by code, version and
normalized citation. Write with mode `0600` in the checked private directory;
leave the old file untouched on failure. Do not echo input values or titles.

`check-titles` reads this output and checks a one-to-one nonblank join for all
licensed controls, including roots and derived keys, and no extra rows for any
edition. Reuse the same key validator in build: unknown rows are errors, while
absent licensed titles keep the existing `title_missing` signing gate. Free
editions can still fall back to their publisher title. The present reader checks
only empty or duplicate exact keys
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
profile. Test output determinism and that extraction failures and malformed rows
never expose a sentinel publisher string in errors or diagnostics. Test poor
title coverage, stale rows, normalized duplicates and failed import leaving the
old CSV intact using an invented `FRAMEWORKS_SOURCE_DIR`. Separately run a local
count and key check against the registered files; record only the approved
per-version totals. No source text, original titles or private artifacts leave
the
[approved places](frameworks-access.md#licensed-material-and-where-it-may-be).
