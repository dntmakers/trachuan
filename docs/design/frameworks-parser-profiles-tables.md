# Control frameworks: parser profiles for table and Vietnamese editions

Edition-specific input, citation and body rules for the AICPA criteria, COBIT
2019, TCVN 11930:2017 and PCI DSS v4.0.1. The shared contract and private
acceptance checks are in [licensed parsers](frameworks-parsers.md); the other
editions are in [parser profiles](frameworks-parser-profiles.md).

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
top edge and the last bare page number. The pinned edition prints no domain
margin label inside these fields; a field line that is only a domain code fails.
Skip the alignment, responsibility, information-flow and other component tables.

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

The parser recognises each label only by this shape. In `-layout` a word is a
whitespace-separated token; lines are one-based and include blank lines, and
columns are one-based runes. In word boxes each word is one box, so the word
count is the box count; x and y are points. Only the first and last words are
compared, exactly as printed except the objective opener label, which is
compared without regard to case. The source fingerprint binds the words between
them.

| Label                    | Words | First to last word                          | Position                                                               |
| ------------------------ | ----- | ------------------------------------------- | ---------------------------------------------------------------------- |
| Objective opener         | 2     | `Governance` or `Management` to `Objective` | flush left, line 6 or 9                                                |
| Focus-area field         | 2     | `Focus` to `Area:`                          | columns 114–125, two or more spaces before, a space then a value after |
| Description, Purpose     | 1     | the label                                   | flush left, alone on the line after trailing spaces                    |
| Purpose stop line        | 15    | `The` to `goals:`                           | flush left, 101 columns, ends with `:` after trailing spaces           |
| Component A              | 3     | `A.` to `Process`                           | boxes in x 39 to below 590, first at x 39–60                           |
| Component A continuation | 4     | `A.` to `(cont.)`                           | as component A                                                         |
| Component B              | 4     | `B.` to `Structures`                        | as component A                                                         |
| Component B continuation | 5     | `B.` to `(cont.)`                           | as component A                                                         |
| Practice header          | 2     | `Governance` or `Management` to `Practice`  | boxes in x 39 to below 300, first at x 39–60                           |
| Metrics header           | 2     | `Example` to `Metrics`                      | boxes in x 300 to below 590, first at x 300–320                        |
| Activity header          | 1     | `Activities`                                | boxes in x 39 to below 496, first at x 39–60                           |
| Capability header        | 2     | `Capability` to `Level`                     | boxes in x 496 to below 590, first at x 496–516                        |
| Page number              | 1     | an unsigned decimal                         | y 742–744, x 36–60 or 550–565; nothing else at y 735 or below          |

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
