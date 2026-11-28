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

Remove the first nonblank line when it is the edition identifier with its `(E)`
suffix. Remove the last nonblank line when it is the ITU-T running footer with
identifier `X.1631 (07/2015)` and a page number on either side. Strip that whole
line before testing headings because its left-side page number can look like a
section number.

Enter the main region at a flush-left `5` heading followed by a `5.1` heading on
the next nonblank line. Switch at the indented standalone `Annex A` marker and
stop at the corresponding `Annex B` marker. Skip front matter, contents, the
annex preamble and all material after that stop.

A main heading starts at column zero after a blank line or at the first content
line of its page. It has a citation with one to three numeric components
starting at `5` through `18`, at least three spaces and a title starting with
`A`–`Z`. A title with a terminal period or a line with dot leaders fails the
heading test. Open a `control` with its immediate dotted prefix as parent, or a
root for a single number. In Annex A, use the same line boundaries and title
checks with at least one separating space. Open `CLD.N.N` as a `control` root
and `CLD.N.N.N` as its `control` child. Require a flush-left standalone
`Control` label on the next nonblank line after each child heading.

Keep direct text, including reference-only sentences, the annex `Control` label
and the cloud-qualified other information section. Keep each retained section
label on its own line. Exclude the comparison and guidance tables. A flush-left,
five-word `Implementation guidance` label ending in `services` opens a table
when the next nonblank line has the two indented three-word cloud role headers.
Require `customer` and `provider` as their respective last words and at least
two spaces between the headers. Skip both columns, including a repeat of the
paired header at the start of a continuation page. Resume at the flush-left,
five-word `Other information` label ending in `services` or at the next heading
or region boundary. A missing paired header or a flush-left line within the
table that matches none of these stops fails the parse.

Trim lines and collapse horizontal whitespace after classifying them. Keep
internal blank lines as paragraph breaks. A list marker such as an en dash
starts a new line. A `NOTE` label starts a new paragraph. Join other wraps with
a space. Ignore page edge blanks. When retained prose continues on the next
page, start a new paragraph after `.`, `:` or `;` unless the next line is a list
item.

## ISO/IEC 27001:2022: `iso27001-2022-pdf`

Use the base PDF. Remove the first nonblank line when it ends with the edition
identifier and `(E)`, including the left-side label on the first content page.
Remove the final copyright line with its page number on either side. Treat
whitespace, U+FEFF, U+0008 and U+200B alone as blank. Keep raw spacing until
after heading and table detection.

Enter the main body at the flush-left `4` heading followed by `4.1` on the next
nonblank line. Stop at the indented standalone `Annex A` marker. A clause
heading is flush left after a blank line or at the first content line of a page.
Require one to three numeric components starting at `4` through `10`, then a tab
followed by U+200B or three to six spaces, then a title starting with `A`–`Z`.
Reject dot leaders and titles ending in a period in the heading test. A matching
line opens a `clause`, using the immediate dotted prefix as parent and a single
number as a root. Prose references stay with their clause. Keep direct body text
and strip U+200B during normalization. Use the
[ISO/IEC 27017 prose joining rules](#isoiec-270172015-iso27017-2015-pdf). List
markers include `a)`, `1)` and an em dash. Keep each `NOTE` marker and its text.

In the annex, skip the introduction and enter the table after the indented
`Table A.1` caption with its dash and title. Skip its parenthesized continuation
caption when it is the first content line of a later page. End the table before
the indented standalone `Bibliography` marker.

An Annex A row opens with a flush-left `5` through `8` identifier, optionally
followed by a dot and a positive integer. The title cell starts at zero-based
column 7 or 10 on that line. Add `A.` to the identifier. A single number opens a
`domain` root with a title and empty body. A dotted number opens an
`annex-control` under its matching domain. The child opener ends in the
standalone `Control` label, separated from the title by at least one space.
Require the label to start in columns 35 through 45. Drop it from the title.

After the opener, a line starting at the opener's title column continues the
title. A line starting in columns 35 through 45 supplies statement text. The
statement column can differ from the label's column on the opener. Carry the row
across a page break until the next opener or end of the table. Join each cell's
lines with spaces. Reject a line outside either column band, a continuation
without an open child row, or a child with an empty title or statement.

## ISO/IEC 27001:2022 Amd 1: `iso27001-2022-amd1-pdf`

The first ISO 27001 PR splits the amendment file currently registered with the
base ([registry:32–43](../../sources/frameworks.yaml#L32-L43)) into a separate
licensed `iso27001:2022-amd1-2024` entry with `amends: '2022'`. Register the
base parser with its own total; enable the amendment parser with its own total
in its PR. Keep the base edition intact.

Select the page starting with the full amendment identifier and `(en)` whose
content includes the flush-left standalone `4.1` target. Require the final
copyright line and its separate decimal page number, ignoring blank lines
between them. Start at `4.1`, skipping the page preamble. Read two blocks in
order, for `4.1` and `4.2`. Each block starts with the bare target citation. The
next nonblank line is a flush-left instruction of exactly ten words: `Add` as
word 1, `sentence` for `4.1` or `note` for `4.2` as word 4, `end` as word 7, and
word 10 ending in a colon. Any other shape fails. Compare instruction words
without regard to case. The other six words are publisher wording and stay out
of code; the source fingerprint binds them, and the shape check guards the
measurement.

The next nonblank line in each block is the full insertion. Require one
flush-left line ending in a period. Require the `NOTE` marker for `4.2` and keep
it as part of the insertion. Emit an `amendment` root with the printed citation,
an empty original title and the insertion text as its body. Trim and collapse
horizontal whitespace while keeping wording and punctuation. End at the footer
after the second insertion. Skip the instructions, covers and other pages.
Reject a missing or extra block or insertion line, any other operation, or a
repeated or unresolved target.

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

Strip the first nonblank edition identifier with its `(E)` suffix and the last
nonblank copyright line with its page number on either side. Ignore U+0008 and
U+FEFF when checking for furniture and blank lines. Start at the flush-left `5`
heading followed by a `5.1` heading on the next nonblank line. Switch at the
indented standalone `Annex A` marker and stop before the indented standalone
`Bibliography` marker. This excludes the front matter, introductory figures and
the framework comparison table before section `5`. Skip the annex preamble
before the first annex heading.

In the main region, a heading starts flush left after a blank line or at the
first content line of a page. Require one to three numeric components starting
at `5` through `18`, one or more spaces, and a title starting with `A`–`Z`.
Reject dot leaders and titles ending in a period in the heading test. Open a
`control` with its immediate dotted prefix as parent, or a root for a single
number. Retain headings with reference-only bodies, including single numbers
with no descendants.

In the annex, apply the same line and title checks to `A.N` and `A.N.N`. An
`A.N` heading opens a `domain` root. An `A.N.N` heading can also immediately
follow its domain heading without a blank line. It opens an `annex-control` only
if the next nonblank line is the flush-left standalone `Control` label. Use the
immediate `A.N` prefix as its parent. Keep domain introductions and direct
control text.

Keep the standalone `Control` label and flush-left `Other information` labels
qualified by both `cloud` and `PII` on their own lines. Use the
[ISO/IEC 27017 prose joining rules](#isoiec-270172015-iso27017-2015-pdf),
recognizing lettered and em-dash list items and `NOTE` and `EXAMPLE` markers.

## Table and Vietnamese editions

The AICPA criteria, COBIT 2019, TCVN 11930:2017 and PCI DSS v4.0.1 profiles are
in
[parser profiles: table and Vietnamese editions](frameworks-parser-profiles-tables.md).
