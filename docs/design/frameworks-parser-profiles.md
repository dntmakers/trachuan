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

## AICPA criteria: `aicpa-tsc-2017-pdf`

Use the
[2022 points-of-focus revision](https://www.aicpa-cima.com/resources/download/2017-trust-services-criteria-with-revised-points-of-focus-2022)
for registry version `2017`. In `-layout`, start at the first two-line table
header, partway down physical PDF page 14. On its first line three uppercase
letters sit 6–10 spaces before the 43-character statement label. The second line
has a capitalized three-letter abbreviation, period, space and `#` at the same
left margin. Read this contiguous run of table pages through page 67, then stop
before the glossary. Recompute the two column starts from the header on each
page. Drop the two header lines, the last `Page N` line and all text from the
first nonblank line left of the statement column that fails the ID-row rule.
This last rule removes footnotes and the text after the final table row.

An ID row starts exactly in the reference column with `CC`, `A`, `C`, `PI` or
`P`, followed by a positive number, a dot and a number. Require at least two
spaces before a nonempty statement at the statement column. A `P<N>.0` row and
all-uppercase category lines at the statement column close the active row and
open no node. Each remaining ID opens a `criterion` root. Keep its statement and
in-cell continuation until the first point of focus. Skip statement-column
lead-ins that announce following or additional points of focus, and their
standalone sublabels. Read lead-ins through their final colon, including
consecutive lead-ins and wraps at the statement margin.

A `•` at the page's bullet column opens a child only while a criterion is
active. Verify one bullet column per page, 9–14 spaces right of the statement
column. Use kind `point-of-focus` and citation `<criterion>-pof-<NN>`, starting
at `01` per criterion. Keep the entire bullet without its opening marker. Use
the text before the first spaced dash in the opening paragraph as its optional
`title_original`, omitting a trailing bracketed category tag from the title.
Keep deeper list items in the same body. Join wrapped lines with spaces, and
keep blank-separated paragraphs and explicit list items. Ignore blank padding
around a physical page break. Carry the open row and bullet across repeated
headers until the next opener or region boundary.

## COBIT 2019: `cobit-2019-pdf`

Use the
[governance and management objectives volume](https://www.isaca.org/resources/cobit).
Read the objective chapters on physical PDF pages 29–296. In `-layout`, open an
`objective` at a flush-left governance or management objective label followed by
`:`, `AAA99` and a title. Match the label case-insensitively. Require the
focus-area field on the right of the same line and the description label on the
next line. Take the title up to the gap before the focus-area field. Open a
`domain` root for each of `EDM`, `APO`, `BAI`, `DSS`, `MEA` before its first
objective.

Keep the description and purpose sections, with their labels on separate lines.
Use the flush-left `Description` and `Purpose` labels as boundaries. Stop the
purpose at the flush-left introductory line for the goals alignment table. That
line starts with `The`, ends with `:` and occupies 101 character columns. Take
only text inside those fields. Drop the running chapter and volume lines at the
top edge, standalone domain margin labels and the last bare page number. Skip
the alignment, responsibility, information-flow and other component tables.

Use `-bbox-layout` for practices because `-layout` can separate the description
and metrics cells by a single space. Only when the verified source SHA-256
matches the pinned
[objectives PDF fingerprint](../../sources/frameworks.yaml#L587-L591), pass
U+0007 to the decoder's per-call replacement option. Keep the pin fixed across
registry changes. Require exactly 1,994 replacements with U+FFFD in the full
decode. Reject U+FFFD in retained word-box text.

In the word boxes, component A starts the process region. Its continuation
heading reopens that region on the next page for the same objective. Component B
ends the region. Inside A, require a governance or management practice header at
`x = 39–60` paired on the same baseline (within 1 point) with its metrics header
at `x = 300–320`. The first nonblank line in the left cell must start with
`AAA99.99` at the left header's x position within 1 point. Require that `AAA99`
equals the open objective. Open one `practice` child per paired header. Set the
right edge of the left cell 4 points before the metrics header's first word.
Retain words starting within the left cell, beginning 1 point before the
practice header's x position, and fail on a word crossing the right edge.
Exclude margin labels by that same band. An ID in an activity, metric or any
other component cannot open a practice.

Use the citation-free remainder of the opener as `title_original`. Keep the
following left-cell description up to the activity header or the page footer. If
the activity header moves to the next page, require the repeated component A
header followed by the activity header there. Exclude activities, capability
levels, related guidance and the metrics column. Join wraps with spaces.
Preserve field breaks and explicit lists.

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

Use the
[2017 Vietnamese edition](https://tieuchuan.vsqi.gov.vn/tieuchuan/view?sohieu=TCVN+11930%3A2017).
In `-layout`, drop the standard identifier as the first nonblank line and the
bare page number as the last nonblank line of each content page. Start at the
flush-left `5` heading on physical PDF page 15, followed by `5.1` as the next
nonblank line. Skip contents entries with dot leaders and a trailing page
number. Stop the main body at the centered Annex A label at column 46 on
page 67. Resume at the flush-left `A.1` heading, after the unnumbered annex
introduction. Read through the last content line of the PDF.

Inside those regions a heading starts at column zero with `5`–`9` and up to
three additional dotted numeric components, or `A.N` or `A.N.N` in the annex.
Use positive components without leading zeroes. Require spaces and a nonempty
title after the citation. The line has a blank before and after it, except that
`A.1` immediately follows the introduction. Keep the rest of the heading line as
`title_original`. A heading with numbered children is a `clause`; a leaf is a
`control`. Use the immediate dotted parent. The main `5`–`9` headings and `A.N`
are roots.

Keep direct Vietnamese text through the next heading or region boundary. Join
wraps with spaces. Retain blank-separated paragraphs and lines beginning with
`-` or a letter and `)`, including Vietnamese list letters. Ignore padding at
physical page breaks. Preserve the source language, spelling and punctuation.

**Approved by the primary (2026-11-22):** the registered file reuses `8.1.3.2`
and `9.1.3.2` for distinct passages. For this exact source fingerprint only,
coalesce each verified pair, retaining both passages in source order (include
the second printed heading in `body`). The first heading supplies
`title_original`. For `N` equal to `8` or `9`, require adjacent `N.1.3.2`
headings between `N.1.3.1` and `N.1.4`. Missing, extra or moved occurrences, or
any other duplicate, fail. Never invent a citation or silently drop either
passage.

## PCI DSS v4.0.1: `pci-dss-4-0-1-pdf`

Use the
[PCI DSS v4.0.1 standard](https://www.pcisecuritystandards.org/document_library/)
and Poppler `-bbox-layout`. Read physical PDF pages 42–372. Within this range,
open `requirement` roots at `Requirement N:` (`1`–`12`) and `Appendix AN:`
(`A1`–`A3`) headings starting at `x = 72 ± 0.1`, `y = 71–110` points. Keep the
title in the heading block. Ignore overview text and lists on these pages. Skip
other pages without a table header. Stop after the last A3 table, before back
matter and worksheets.

The table's outer header pairs the combined requirements/testing and guidance
labels on a baseline within 0.5 points. Their first words start at `x = 185–205`
and `580–605`, at `y = 79–123`. This includes the lowered A2 header. Read table
content below the outer header's bottom and above the page's first footer line.
The footer starts with the standard identification line in the bottom band at
`y >= 530`. The next line carries copyright. Skip both footer lines and their
right-edge date and page number.

Inside the table, pair each requirements subheader with the testing subheader on
the same baseline within 0.5 points. A page can repeat this pair below a
continued row. Let `L` and `T` be the pair's first word x positions, rounded to
0.1 point. `L` is 59.8, 77.8 or 78.0. `T` is 284.8, 290.8 or 298.0–298.3.
Require each pair on a page to give the same bands. For a page without a pair,
reuse the bands from the last page with a matching outer header, comparing both
outer x positions rounded to 0.1 point within this edition. Let `R` be the sum
of the combined outer header's left and right x edges, minus `L`, plus 4 points.
Keep word boxes wholly inside `[L-5, T-3]`. Exclude the testing and right
guidance bands beyond `T-3` and `R` respectively. Fail on any word crossing
either cut outside a spanning group heading. Discard both subheaders.

Between the outer header and the first paired subheader, a two-component ID at
the first line of a block and `x = L-1.5` to `L+6` opens a spanning group. The
whole block provides its title, including wraps across column cuts. Below a
paired subheader, open a row only when the first word of a block's first line is
a complete ID in the left band and starts in the same narrow x range. Use `N.N`
through five numeric components, or the corresponding `AN` prefix. Require
positive components without leading zeroes and a matching active root. Every
group and row is a `requirement`. Its immediate dotted parent must already
exist. A number beginning a later line of an open block stays body text.

At the top of a table, any left text before a new heading continues the active
row. An explicit ID followed by `(continued)` is a caption. Require an earlier
row with that ID under the current root. If the caption's left segment contains
text, the ID must match the active row. Append the left text after removing the
caption. Drop a caption with an empty left segment even when its ID differs from
the current row. Only excluded columns continue in that case. Neither a repeated
outer header nor a caption creates a node. Fail on unowned left text, unknown
captions and duplicate openers.

In row bodies keep the text after the ID and the local objective and
applicability sections when present. Start each section on its own line. Within
the left band, read by y position and then x position. Attach an isolated list
glyph to the adjacent line to its right when their tops differ by at most 2
points. Keep list items and paragraph breaks between blocks, joining wraps with
spaces. Page breaks alone leave the paragraph open. Roots and spanning groups
keep their headings as titles and have empty bodies.
