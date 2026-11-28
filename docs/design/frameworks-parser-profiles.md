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

Drop page furniture first: the running header as a page's first nonblank line,
the copyright footer line with or without its page number, and lines of only
whitespace and U+FEFF. Start at the first flush-left `5` domain heading that is
not a contents entry (contents entries carry dot leaders). Stop before the
centered `Annex A` heading. A flush-left `N` heading (`5` to `8`) whose next
nonblank line is an `N.1` heading opens a `domain` root. A flush-left `N.N`
heading opens a `control` child only when its attribute table header
(`Control type`) follows within five nonblank lines; otherwise it is body text.
Exclude the attribute table up to the flush-left `Control` label. The labels
`Control`, `Purpose`, `Guidance` and `Other information` open the sections of
the body, kept in order, each label on its own line and sections separated by a
blank line. Tables inside guidance stay as text lines. Exclude the annex tables.

The extracted text has no blank lines between paragraphs. Collapse runs of
spaces and trim each line. A list marker (`a)`, `1)`, `—`, `•`) starts a line;
otherwise a line joins the previous one with a space unless that one ends with
`.`, `:` or `;`. A page break alone never starts a paragraph.

## ISO/IEC 27017:2015: `iso27017-2015-pdf`

Start at the first numbered control-guidance section after front matter and stop
before bibliography/back matter. Reject page-footer numbers before heading
detection. Keep numbered sections `5`–`18` and their printed subsections as
`control` nodes, parented by the immediate dotted prefix. In the cloud annex
also keep `CLD.N.N` roots and their `CLD.N.N.N` children as `control`. Keep
direct narrative guidance and explicit reference-only text as printed; never
pull in ISO 27002 text to fill a reference. Omit standalone comparison or
guidance tables.

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

## Table and Vietnamese editions

The AICPA criteria, COBIT 2019, TCVN 11930:2017 and PCI DSS v4.0.1 profiles are
in
[parser profiles: table and Vietnamese editions](frameworks-parser-profiles-tables.md).
