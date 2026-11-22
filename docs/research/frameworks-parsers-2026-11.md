# Licensed framework parser measurements, November 2026

The local files yield the totals below. None of the scoped registry entries sets
`expected_controls` yet; the amendment has no separate entry. These are proposed
registry values, not passing runtime count checks. The primary reviews measured
totals before each parser PR enables its registry entry.

## Method

On 2026-11-22, read the registered files from the private source directory.
Verify byte size and SHA-256 before extracting. With local Poppler 26.11.0,
compare extraction from stdin with extraction by file path. Output bytes match
for every measured PDF. `-layout` preserves the rows in the prose documents; PCI
requires physical block and column positions (`-bbox-layout`). Use the profiles
in [licensed parsers](../design/frameworks-parsers.md) to count opening
citations, exclude headings in other regions and check parent prefixes. Count
the workbook's primary sheet directly. No PDF or workbook cell text was written
to disk, printed or copied into a fixture.

The publisher links identify the editions and sources. The public pages were not
fetched during this offline measurement. No public source was verified for these
exact catalog totals, which include structural nodes; the count column is
measured and needs primary approval.

| Edition                                                                                                                        | Registry value                                             | Measured and proposed |
| ------------------------------------------------------------------------------------------------------------------------------ | ---------------------------------------------------------- | --------------------: |
| [PCI DSS v4.0.1](https://www.pcisecuritystandards.org/document_library/)                                                       | [unset](../../sources/frameworks.yaml#L293-L313)           |                   366 |
| [ISO/IEC 27001:2022](https://www.iso.org/standard/27001)                                                                       | [unset](../../sources/frameworks.yaml#L18-L43)             |                   138 |
| ISO/IEC 27001:2022 Amd 1:2024                                                                                                  | [no separate entry](../../sources/frameworks.yaml#L32-L43) |                     2 |
| [ISO/IEC 27002:2022](https://www.iso.org/standard/75652.html)                                                                  | [unset](../../sources/frameworks.yaml#L60-L80)             |                    97 |
| [ISO/IEC 27017:2015](https://www.iso.org/standard/43757.html)                                                                  | [unset](../../sources/frameworks.yaml#L81-L101)            |                   176 |
| [ISO/IEC 27018:2019](https://www.iso.org/standard/27018)                                                                       | [unset](../../sources/frameworks.yaml#L118-L138)           |                   132 |
| [AICPA criteria](https://www.aicpa-cima.com/resources/download/2017-trust-services-criteria-with-revised-points-of-focus-2022) | [unset](../../sources/frameworks.yaml#L240-L260)           |                   391 |
| [COBIT 2019](https://www.isaca.org/resources/cobit)                                                                            | [unset](../../sources/frameworks.yaml#L572-L597)           |                   276 |
| [CSA CCM v4.1](https://cloudsecurityalliance.org/research/cloud-controls-matrix)                                               | [unset](../../sources/frameworks.yaml#L493-L539)           |                   224 |
| [TCVN 11930:2017](https://tieuchuan.vsqi.gov.vn/tieuchuan/view?sohieu=TCVN+11930%3A2017)                                       | [unset](../../sources/frameworks.yaml#L598-L617)           |                   290 |

## Cross-checks

- PCI and COBIT totals match the archived compliary parser count pins
  (`pkg/normalize/pci_test.go:439–447`, `pkg/normalize/cobit_test.go:339–395`).
  PCI's first block line in the left column separates an opener from a wrapped
  reference; the A2 table uses a lower header position. COBIT practice
  references reappear outside the process component.
- ISO 27001 base, ISO 27002 and CSA CCM match the archived count pins
  (`pkg/normalize/iso_test.go:546–549, 662–664`,
  `pkg/normalize/ccm_test.go:329–331`). The ISO 27001 amendment targets existing
  base clause citations. The CSA workbook includes IDs with `&` in the prefix.
- ISO 27017 matches the archived total (`docs/design/SCHEMA.md:216–219` in
  compliary). The ISO 27018 count differs: it includes printed Annex A parent
  headings and normative headings missed by the archived extraction.
- AICPA's catalog ends before the glossary. The archived extraction
  (`pkg/normalize/tsc.go:209–264`, `pkg/normalize/tsc_test.go:323–329` in
  compliary) consumes bullets after the criteria table. Those bullets do not
  open points-of-focus controls.
- TCVN 11930 has usable Vietnamese text and is not an OCR item. Its proposed
  total counts distinct printed citations, under the coalescing rule in the
  profile. It agrees with the archived total (`docs/design/SCHEMA.md:225` in
  compliary); agreement does not establish that the previous extraction retained
  every passage.

The private seed join passes for CSA CCM, ISO 27002, ISO 27017 and COBIT. ISO
27001 and its amendment, ISO 27018, PCI and TCVN 11930 need reviewed paraphrases
for uncovered nodes. AICPA needs stale rows removed. No publisher heading or
seed title is reproduced here. These checks establish the citation inventory,
not production parser correctness or text completeness.
