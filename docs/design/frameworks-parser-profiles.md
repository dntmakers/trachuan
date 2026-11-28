# Control frameworks: parser profiles

Edition-specific input, citation and body rules. The shared contract and private
acceptance checks are in [licensed parsers](frameworks-parsers.md).

## CSA CCM v4.1: `csa-ccm-4-1-xlsx`

Select the CCM workbook, not CAIQ. The CSA reader selects the exact `CCM` sheet
and reads the workbook metadata, relationships, selected sheet and, when needed,
shared strings directly from the ZIP. Those parts share a 256 MiB uncompressed
budget; decoded cell values have a separate 256 MiB total budget after shared
string references are expanded. Reject duplicate ZIP parts, case-insensitive
sheet-name collisions, invalid XML roots or trailing data, invalid cell types or
coordinates, out-of-order cells, and overlapping merges. Decode inline and
shared strings and OOXML escapes before normalization; use raw cell values, not
formatted values. Cells inside a merge other than its anchor must be blank.

Row 1 is preamble. Row 2 has three merged groups, `F2:H2` for service models,
`I2:N2` for architecture and `O2:W2` for organizational relevance. Require those
exact ranges, with a nonblank group label in each anchor cell and all other row
2 cells blank. Row 1 may hold merges. A domain header row may be merged across
exactly A to E (`A4:E4`); rows from the pinned trailer row on may be merged. Any
other merge, including one on a control row, fails. Row 3 gives the columns:

| Columns | Meaning                                         |
| ------- | ----------------------------------------------- |
| A–D     | Domain, title, ID, specification, in order      |
| E       | Standalone Lite applicability                   |
| F–H     | IaaS, PaaS, SaaS, in order                      |
| I–N     | Architectural relevance columns in sheet order  |
| O–W     | Organizational relevance columns in sheet order |

Require nonblank column labels and reject duplicate labels within a group,
missing columns, or changed merge ranges before emitting nodes. A domain header
in A with no ID in C opens a `domain` root keyed by its terminal uppercase
acronym, including `&`. A row with an ID in C is a `control` (`<acronym>-NN`),
parented to the active domain with that acronym. Keep the title in B and
specification in D. Reject malformed IDs, missing title or specification, a
mismatched domain or an unexpected row inside the table. Skip preamble and blank
rows and do not parse other sheets. The profile defines no trailer grammar. Skip
trailer rows only for the pinned workbook, identified by its whole-file SHA-256,
from its pinned first trailer row to the end. In any other workbook a nonblank
row that is not a domain or control fails the parse.

After the specification and a blank line, emit one line per E–W cell, in column
order. Use `Group / Column: value`, taking the group from its row 2 merge anchor
and the column from row 3. Use `Column: value` for E. Trim the fields and
replace line breaks within a field with spaces. A blank value ends at the colon,
with no trailing space. A nonblank false or zero stays a value. An invented XLSX
fixture must contain all three actual merged ranges, distinct invented labels,
blank and false cells, and check the exact serialized order. Reject a variant
with a missing or shifted merge.

## ISO/IEC 27002:2022: `iso27002-2022-pdf`

Start at the first thematic heading in the body, not its contents entry. Stop
before annexes. The flush-left `5`–`8` headings give `domain` roots; `N.N`
headings give `control` children. Keep control statements, narrative guidance
and references in their own section, not the separate attribute/tag matrix or
annex tables. A dotted citation in a paragraph is not a heading.

## ISO/IEC 27017:2015: `iso27017-2015-pdf`

Start at the first numbered control-guidance section after front matter and stop
before bibliography/back matter. Reject page-footer numbers before heading
detection. Keep numbered sections `5`–`18` and their printed subsections as
`control` nodes, parented by the immediate dotted prefix. In the cloud annex
also keep `CLD.N.N` roots and their `CLD.N.N.N` children as `control`. Keep
direct narrative guidance and explicit reference-only text as printed; never
pull in ISO 27002 text to fill a reference. Omit standalone comparison or
guidance tables.

## AICPA criteria: `aicpa-tsc-2017-pdf`

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

## COBIT 2019: `cobit-2019-pdf`

Use the objectives volume only. Recognize objective headers in the objective
chapters, then delimit each objective's process component before reading left
cells of its practice table. The `EDM`, `APO`, `BAI`, `DSS`, `MEA` prefixes are
`domain` roots, emitted before their first child. `AAA99` is an `objective`
child of its domain; `AAA99.99` is a `practice` child of the matching objective.
Keep the objective's direct description/purpose fields and each practice's
description cell. Ignore activity lists, metrics, capability levels,
responsibility and information-flow tables, and other components. A practice
reference outside its primary process table never opens a node.

## ISO/IEC 27001:2022: `iso27001-2022-pdf`

Use the base PDF only. Read normative clauses `4`–`10` after front matter and
before Annex A. Flush-left headings open `clause` nodes, split by their printed
dotted prefix. Keep direct clause text; skip page numbers, references in prose
and contents entries. In the Annex A control table, open `domain` roots `A.N`
and `annex-control` children `A.N.N` from the identifier column; add `A.` to the
number printed in the table. Read the adjacent heading and statement cells
across wraps and page breaks. Table labels and bibliography are not text of a
control.

## ISO/IEC 27001:2022 Amd 1: `iso27001-2022-amd1-pdf`

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

## ISO/IEC 27018:2019: `iso27018-2019-pdf`

Read the `5`–`18` guidance sections and children as in 27017, without CLD. Keep
every printed numbered heading in the normative body, even if the text only
points to another edition. Enter the data-protection annex separately: its `A.N`
headings open `domain` roots and `A.N.N` headings open `annex-control` children.
Keep direct narrative text; exclude framework comparison tables, introductory
figures and bibliography. Do not synthesize missing parents or replace printed
headings with the prior catalog's selection.

## TCVN 11930:2017: `tcvn11930-2017-pdf`

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

## PCI DSS v4.0.1: `pci-dss-4-0-1-pdf`

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
