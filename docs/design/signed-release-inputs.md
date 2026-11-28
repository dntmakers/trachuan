# Signed release inputs on the MacBook

Reviewers and framework builds use the exact law corpus that the release
workflow signed. Downstream export uses the same verifier with the frameworks
signer. Fetching is a separate step so these commands can run with the network
disconnected and can use the same inputs again.

## Trust and verification

Use one Go verifier in `corpus` that invokes the pinned Cosign executable. The
host and signing runner already use Cosign, so all three can test the same
Sigstore bundle and certificate policy. An in-process Sigstore library would
remove an executable from setup. It would also make Go own verification options,
certificate claim handling and Sigstore upgrades.

Run the installed `corpus` binary built from a trusted `main` commit. Embed
`deploy/host/deploy-policy.json` and the reviewed MacBook Cosign SHA-256 pins
from `deploy/sigstore/cosign-checksums.json` at build time. The source files own
the policy and pins. A test compares the embedded bytes with both files. The
binary uses those bytes regardless of its working directory, environment or
release inputs. The deploy policy supplies the accepted repositories, issuer,
signer expressions, registry channels, extension allowlist and Cosign version.
The caller selects law or frameworks before reading the manifest. Use
`corpus_identity` for law and `frameworks.identity` for frameworks, keyed by the
allowed manifest repository. The workflow ref for both is `refs/heads/main`.
Require the manifest's commit to match the certificate's workflow SHA.

Install the executable from the
[Cosign v3.1.4 release](https://github.com/sigstore/cosign/releases/tag/v3.1.4).
`CORPUS_COSIGN` gives its absolute installed path outside worktrees. Before each
invocation, check the executable against the embedded SHA-256 for the platform
and require the pin's version to match the embedded policy. Refuse any mismatch.

During `corpus release-input fetch`, run the pinned Cosign online:

```sh
cosign trusted-root create --with-default-services --out <staged-root>
```

The binary sets `TUF_MIRROR=https://tuf-repo-cdn.sigstore.dev` to select
Cosign's embedded public-good bootstrap and bypass a cached `remote.json`
mirror. Set `TUF_ROOT` to the verifier's private TUF cache. Give Cosign a
minimal environment with neither caller-supplied TUF variables nor Sigstore
overrides.

The verifier manages its TUF cache and `trusted_root.json` under
`~/Library/Application Support/trachuan/release-verifier/`, using the OS
account's home directory. Keep that state outside worktrees and release inputs.
On creation and every reopen, verify that the managed directory and cache have
mode `0700`, and that `trusted_root.json`, when present, is a regular file with
mode `0600`. Require the local account to own all three and refuse ACLs that
grant another account access to the managed directory, cache or root file.
Resolve the paths without following symlinks. Refuse symlinks, cache entries
owned by another account, and cache entries writable by group or others.
Serialize refreshes and atomically replace the root with checked output from
Cosign. A failed refresh fails the fetch and preserves the last authenticated
root. Offline commands read the checked managed root and fail if it is absent or
lacks the keys needed for a bundle.
[Cosign v3.1.4 root creation](https://github.com/sigstore/cosign/blob/v3.1.4/doc/cosign_trusted-root_create.md)
[Cosign v3.1.4 TUF setup](https://github.com/sigstore/cosign/blob/v3.1.4/pkg/cosign/tuf.go#L69-L138)

Read the manifest and bundle once into bounded buffers and give both buffers to
the shared verifier. Validate the canonical JSON and exact versioned shape from
[law signing](corpus-signing.md#corpus-manifest) or
[frameworks signing](frameworks-release.md#signing-workflow), including the
manifest's kind, repository, database, dump object key and release identity.
Refuse duplicate keys, unknown fields outside the versioned schema, unsupported
formats and a nonpositive sequence. Invoke Cosign directly with a deadline and a
minimal environment. Send the checked manifest bytes on stdin, and give Cosign
private copies of the bundle and trusted root with the following arguments:

```sh
cosign verify-blob --bundle <bundle> --trusted-root <root> \
  --certificate-identity-regexp <policy-expression> \
  --certificate-oidc-issuer <policy-issuer> \
  --certificate-github-workflow-repository <manifest-repository> \
  --certificate-github-workflow-ref refs/heads/main \
  --certificate-github-workflow-sha <manifest-commit> -
```

Require a Sigstore bundle in the current protobuf JSON format and keep
transparency log and certificate verification enabled. With a local bundle and
`--trusted-root`, Cosign can verify offline. The `--offline` flag alone can
still fetch trust material through TUF. Refuse on any Cosign failure, including
timeout. Return the parsed manifest, both original buffers and the SHA-256 of
the manifest buffer only after Cosign succeeds. Fetch writes, snapshot hashes
and freezes, and export copies the returned buffers. Report verification
failures without input bytes.
[Sigstore blob verification](https://docs.sigstore.dev/cosign/verifying/verify/)
[Cosign v3.1.4 arguments](https://github.com/sigstore/cosign/blob/v3.1.4/doc/cosign_verify-blob.md)
[Cosign v3.1.4 trust selection](https://github.com/sigstore/cosign/blob/v3.1.4/cmd/cosign/cli/verify/common.go#L172-L199)

## Prepare a release directory

```sh
corpus release-input fetch <cc> <rel> --out <dir> [--local-dump <file>]
corpus release-input fetch frameworks <rel> --out <dir>
```

The new fetch command resolves `<cc>-<rel>` or `frameworks-<rel>` in the
policy's channel with ORAS v1.3.6, the same version as the signing runner.
Resolve once, then fetch by digest. Apply the host registry's checks for the
corpus OCI artifact type, exact manifest and bundle layer names, and advertised
layer sizes and SHA-256. Use the shared Go verifier with the requested country
and release before using any manifest value to fetch the dump. `resign-check`
verifies a manifest for workflow re-dispatch and prints a dispatch command.
Fetch preserves the verified bytes for subsequent commands.

For law, pass the retained upload copy as `--local-dump` when available to avoid
transferring the dump again. Otherwise fetch the verified object key from the
law bucket in policy, using the existing `trachuan-release-upload` AWS CLI
profile and configured OCI S3 endpoint. The fetch command invokes only read
operations. For a download, require HEAD size to match the signed size, then
apply the signing workflow's conditional range and response checks. Send the AWS
CLI body through a private pipe and read response metadata separately. Stop and
cancel the download on the first byte beyond the signed size. For either source,
stream into staging and verify size and SHA-256 before publishing. Use the
[signing workflow's dump size cap](corpus-signing.md#signing-workflow) and the
[updater input limits](budgets-host.md#updater-input-limits) for manifest and
bundle sizes. Fail on a missing object, short or oversized stream, mismatched
hash or insufficient space. Include the disk cost of one local dump copy and,
for review or snapshot, one scratch restore. Frameworks fetches the signed pair
only. A frameworks dump stays in the places specified by
[frameworks access](frameworks-access.md#licensed-material-and-where-it-may-be).
[AWS CLI streaming output](https://github.com/aws/aws-cli/blob/2.40.0/awscli/customizations/streamingoutputarg.py)

| Input directory                  | Exact files                                              |
| -------------------------------- | -------------------------------------------------------- |
| Law review or build              | `manifest.json`, `manifest.sigstore.json`, `corpus.dump` |
| Frameworks predecessor or export | `manifest.json`, `manifest.sigstore.json`                |

An input directory is separate from an upload or build directory. Require
absolute paths and refuse symlinks in input or output paths, including the
`--local-dump` source. Refuse additional or missing input entries and
non-regular files. Open relative to the checked directory and keep the opened
files or checked bytes through use. Fetch stages private files beside a new
output directory, syncs them and publishes the directory atomically without
replacement. Remove partial output after a failure. Frameworks paths follow the
[build's work path rules](frameworks-build.md#runs-resume-and-failure).

The manifest must match the explicitly selected country and release for law, or
the explicitly selected frameworks release. A release tag is a discovery hint.
The local commands accept any supported signed release with a positive sequence,
including a historical release. They keep its sequence and digest as evidence of
the selected signing, and neither consult a production tag nor apply activation
floors. Production selection, advancement and import replay rules stay with
their respective consumers. Once prepared, the input directory needs no
registry, object storage or Sigstore network access to use or recheck.

## Restore a law input

The review command and the framework snapshot share a restore and extraction
path. Verify the signature first. The first law projection reader supports only
[schema version 9](corpus-schema.md). Refuse any other schema before starting a
restore. Verify the exact opened dump's size and SHA-256 before using it.

Reuse `corpus_builder` from `deploy/local/db-setup.sh` and the existing local
temporary database helper with its lock and ownership marker. Before creating a
scratch database, require `CORPUS_BUILDER_DATABASE_URL` to connect over a local
Unix socket or loopback. Check all connection candidates and pin the verified
local endpoint for `pg_restore`. Query `pg_catalog.pg_roles` on the open
connection and require both the session and current role to be `corpus_builder`,
with `LOGIN`, `CREATEDB`, `NOSUPERUSER` and `NOCREATEROLE`. Refuse a remote
endpoint or role mismatch before creating the database. Require the expected
extension names and versions. Use `CORPUS_PG_RESTORE` and the law TOC rules from
[corpus activation](corpus-release-activation.md#steps) with
`--no-owner --no-privileges --exit-on-error`. Check the restored catalog before
reading data. The signature is the trust boundary for SQL execution. The TOC and
catalog checks limit what the reader accepts.
[PostgreSQL 18 pg_restore](https://www.postgresql.org/docs/18/app-pgrestore.html)

For tools that need a path, provide a run-private checked copy or a stable open
file. Apply the
[run's byte binding](frameworks-build.md#runs-resume-and-failure) before and
after each tool call. In one read-only repeatable-read transaction, require a
single `public.release` row matching the manifest's country, `rel`, schema
version and all four recipes. Read the named law targets from that same
transaction using the
[reviewed-link projection](frameworks-links.md#reviewed-control-to-law-entries).
Refuse mismatches before reading targets. After extraction, close the
transaction and drop only the marked scratch database. A crash can leave a
marked database; recovery must check the same ownership and lock before cleanup.
No existing release database is a restore target. Restore and cleanup failures
fail the command before it publishes a report or snapshot.

## Proof and rollout

The first Go verifier ships only when a shared fixture suite passes through the
actual Go, host Python and signing runner Python manifest readers. Feed all
three readers the same canonical bytes for supported law versions, frameworks
and, when introduced, the signed `downstream` shape. Include duplicate keys,
noncanonical encoding, schema-gated fields, unsupported versions and malformed
recipes. Apply the [law manifest read rules](corpus-signing.md#corpus-manifest)
in all three readers. Include valid `rel` and `previous` with and without
suffixes. Refuse impossible dates in either field, year zero, zero and
leading-zero suffixes, empty recipe objects, and a nonempty `dense` object with
missing or malformed `query_hash`. In that same slice, fix
`deploy/github/corpus_manifest.py` to apply these rules when reading existing
manifests. Check the full frameworks recipe shape on predecessor reads in
`deploy/github/frameworks_manifest.py` and require all three readers to refuse a
recipe outside that shape. Compare normalized Cosign arguments for each kind,
including the runner's exact identity and the host's anchored expression. Prove
that a worktree cannot replace trusted policy or checksums by exercising the
installed binary from a checkout with altered copies.

Run real Cosign through Go and both Python paths against synthetic bundles and a
test root. Inject the root through test harnesses to give all three paths the
same trust material. Require matching decisions and manifest hashes for valid
releases. Reject tampered bytes, missing proof, wrong issuer, workflow,
repository, ref and commit, and a valid signature for the other corpus kind. Run
Go verification with the network unavailable. Cover wrong or missing managed
roots, a failed refresh, a cache `remote.json` naming another mirror, and a
symlinked `trusted_root.json`. Prove refresh uses the pinned mirror and that
fetch and offline verification refuse the symlink before invoking Cosign. Cover
legacy bundles. Use invented law and control content for dump and projection
tests.

Prove that a signed schema 8 law manifest passes signature verification and that
the law projection reader refuses it before opening a database connection.
Exercise refusal of a nonlocal endpoint or changed builder role before database
creation. Check symlinks, extra files, truncated or swapped dumps, and wrong
restored identity or recipes. Swap original manifest and bundle paths after
verification and prove snapshot and export use the verified buffers. Prove that
the review and build select the same law facts, that a re-sign under the same
`rel` changes the manifest binding, and that a retry uses the frozen projection.
Cover a build without reviewed links and an export without a dump.

Ship the trust setup, fetch and verifier before the commands that use them.
Local law restore uses the existing `corpus_builder` role and adds no privilege.
Rollout for this restore waits on the same pending owner approval for `CREATEDB`
that covers local law and frameworks builds. See
[database roles](system.md#database-roles) and
[index release order](index.md#release-order-and-evidence).

Ship the law-only and framework readers before admitting the first
`control_law_link` entry. Before signing the first downstream frameworks
release, align both Python verifiers and Go with that signed shape and follow
the [frameworks release order](frameworks-release.md#order). Verify a supported
historical law release on the MacBook with the network disconnected before using
the path for a real review.
