# Frozen extract admission

Admit an explicit v1 extract artifact only when its serving source and detector
inputs agree with the verified frozen inputs. Use the same rule for a new
artifact, a cache hit and concurrent publication. A matching input hash alone
cannot attest to derived observations.

Check deterministic tree and transcription precedence against the verified fetch
bundle before using a frozen OCR row. Do not look for a new OCR candidate. Check
the configured engine and checker, linked PDF manifest member, text and check
identities, and planned input hash. The verdict and waiver must permit the
recorded serving choice. Hash the stored OCR text on read. Verify the check
result using the store's canonical serialization, preserving the legacy omission
of `dropped_equal`, not PostgreSQL's JSONB rendering
([OCR check](../../apps/corpus/internal/ocrstore/check.go),
[OCR store](../../apps/corpus/internal/ocrstore/store.go)).

Produce the detector inputs from those verified bytes. When accepting an
existing artifact, verify the exact `text.json` bytes read against its manifest
and require the recorded source and complete typed detector inputs to equal the
fresh derivation. An inconsistent observation is a failure, including one that
keeps all identity hashes. Preserve legacy extraction bytes and behavior.

Parse the OCR text by iterating over lines and whitespace-delimited fields
without allocating line or word lists. Keep NFC normalization and raw-line
locators unchanged. Reuse the bounded paragraphs and scopes for counts and
matching.
