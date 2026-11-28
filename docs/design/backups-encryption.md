# Client-side backup encryption

Proposed change to [ADR 0006](../adr/0006-single-host-oci-podman.md). Encrypt
pgBackRest backups and archived WAL on the PostgreSQL host so a leaked read
credential for the encrypted bucket alone exposes neither account nor licensed
framework rows. Keep Oracle-managed bucket encryption. The
[framework access note](frameworks-access.md#licensed-material-and-where-it-may-be)
owns where licensed material may exist. See
[recovery and proof](backups-encryption-recovery.md).

## Repository and cipher

Keep stanza `trachuan` and create an empty repository in a separate private
Standard bucket, `trachuan-backups-enc-001`, in `ap-singapore-1`, at repository
path `/pgbackrest/trachuan`. Each generation has its own bucket, writer and
reader identities, and Vault passphrase. Keep an owner-controlled inventory
available after host loss. For each generation record its bucket, path, stanza,
cipher, exact key version, credential references, compatible image and recovery
coverage. Use the bucket name in the inventory and in proof and health results.
A pgBackRest index such as `repo1` is a config position.

Set `repo1-cipher-type=aes-256-cbc` for the active repository and supply
`repo1-cipher-pass` in its private runtime include. pgBackRest encrypts before
sending objects to S3 and requires a new repository to change encryption
settings. The passphrase protects `backup.info`, `archive.info` and their
copies. They hold the generated secrets that protect the backup manifests and
archived WAL. Each full backup's manifest in turn holds the secret that protects
the backup data files. [Cipher type][cipher-type] · [Passphrase][cipher-pass] ·
[Encryption setup][cipher-setup] · [Stanza code][stanza-source] · [Backup
code][backup-source] · [WAL archive code][wal-source]

Bucket readers still see object names, sizes and times, including backup labels
and WAL segment names. The cipher leaves PostgreSQL data files, local WAL,
recovered WAL in the spool and restored data under their existing host
protections. Bucket write access can destroy backups. A compromised PostgreSQL
container or host can reach both data and key material. Keep TLS verification
and scope credentials to each bucket. Server-side encryption serves plaintext to
authorized bucket readers. [OCI encryption][oci-encryption] · [Repository
listing][repo-list] · [Spool behavior][spool]

A new prefix in the existing bucket could also house an encrypted repository.
Separate buckets let the owner withdraw the previous writer and scope read-only
access to one repository with bucket-level IAM. A direct switch before the new
repository's first full would place recent recovery targets outside its
coverage. Dual archiving keeps the previous writer available while we prove both
recovery paths. [OCI object permissions][oci-object-iam] · [pgBackRest
archive-push][archive-push]

## Passphrase custody

The owner generates 32 random bytes as 64 lowercase hex characters with
`openssl rand -hex 32`. Feed the value directly to protected input for OCI Vault
and an encrypted, owner-only offline recovery copy outside the host and OCI
account. Label both copies by repository. Keep values out of shell arguments,
history, terminal output, the clipboard, logs, images, Git, GitHub and OpenTofu
state. [OpenSSL rand][openssl-rand]

Store the first passphrase as `backup_repo_cipher_001` in the existing Singapore
vault and protect it with its existing software-protected key. Use a new Vault
secret per generation, pin its OCID and version, and disable automatic rotation.
The host instance principal reads only secrets the active configuration needs.
The S3 identities get bucket permissions only. The owner approves IAM changes
for a replacement bucket or secret. [OCI Vault version retrieval][oci-secrets]

Reuse [host secret delivery](deployment.md#secrets) and fetch only the pinned
version. Pipe it into the `trachuan` user's Podman file store. The PostgreSQL,
backup setup and authorized restore commands mount it as UID 999, mode 0400. The
wrapper checks the 64-character hex value and writes the cipher option and S3
keys to `/run/pgbackrest/conf.d/secret.conf` on private tmpfs, owner 999 and
mode 0600. Only the path enters the environment. The Podman file store persists
the key on the host and PostgreSQL can write key material to swap. Keep the
running configuration on missing or invalid fetch and raise `secrets_failed`.
Fail startup on an encrypted configuration without a valid key. Recreate the
container and prove decryptability before accepting changes. [Podman secret
creation][podman-secret] · [Podman mounts][podman-run]

## Cutover and retention

Use the backup lock to serialize signed deployment, backup, restore and corpus
activation. Keep one durable writer record with a compatible image floor and at
most one approved transition. Sync each atomic update and its parent directory
to disk before starting the next writer. The allowed writer set is current
generation A alone, exactly A+B during overlap, then B alone. Bind each
generation to its bucket, path, stanza, cipher and exact key version. The
startup guard and updater check the record before installing or starting a
writer, including during reboot, rollback and recovery. Missing, invalid or
inaccessible state after enrollment holds ingress and keeps WAL queued until the
operator recovers the record. Keep an owner copy for bootstrap. Preserve the
forward-only state across rotations and reject retired generations as writers.

1. Provision the new bucket, credentials, Vault secret and escrow copy. Use the
   pinned image and a one-repository config to initialize it with
   `stanza-create --no-online` and read-only access to the live `pg_control`.
   Finish before the updater starts its first `check` across both repositories.
2. Under the lock, stop the A writer and atomically record the approved overlap
   before installing or starting A+B. Configure B as `repo1` and A as `repo2`
   with their recorded credentials and ciphers. Restart with a fresh async spool
   so an earlier acknowledgement cannot stand in for a dual push. `archive-push`
   writes both and `check` tests both. An archive failure in either leaves WAL
   queued. Use explicit `--repo` for `backup`, `expire`, `annotate`, `info`,
   `verify`, `restore` and `archive-get`. Suspend expiry on the previous
   generation. Pin and verify pgBackRest 2.60.1 in the running image and job
   containers before dual writing. In 2.60.0, `annotate` without `--repo` could
   overwrite the default repository's `backup.info`. [Command
   reference][commands] · [2.60.1 release notes][pgbackrest-release]
3. Take and verify a new full. Prove PITR from each repository alone. Record the
   UTC boundary, backup labels, WAL segment and timeline. Show that the previous
   generation covers up to the boundary and the new one covers from it. If the
   pilot fails during overlap, stop the dual writer under the lock and record an
   explicit abort to A alone. Keep the floor and retired-generation history. Use
   a fresh spool and prove A's WAL and backup health.
4. Under the lock, stop the dual writer and atomically commit B as the sole
   writer before installing or starting B alone. Record A as retired and carry
   forward the maximum compatible image floor. Use a fresh spool. Recovery now
   rolls forward with B. Commit the normal [release floor](release-floor.md) on
   successful activation. The current updater starts PostgreSQL before that
   commit. [`deploy_activate.py`](../../deploy/host/updater/deploy_activate.py)
   Kill the updater immediately after recording B alone and again after the
   first B-only segment, before the activation commit. On restart, prove the
   queued WAL reaches B while stale acknowledgements remain in the old spool.
   Rehearse a full second rotation from B to C. Reboot in the B-only, B+C and
   C-only phases. In each phase prove the exact writer set receives WAL and
   stale bundles, retired writers and unapproved pairs are refused.
5. Verify the new-only `check` and restore, then revoke the previous writer's
   keys and grants and remove its secret mounts and Vault read grants. Keep
   owner read-only access separate from deletion. For a retired encrypted
   generation, prove restore from escrow, remove its secret from reconciliation
   and delete its Podman copy. Verify absence after a forced reconciliation and
   reboot. Keep the custodied copies for recovery. [Podman secret
   removal][podman-secret-rm]
6. Record when previous writes stopped. Retain that bucket read-only for at
   least 30 days. Readers of the plaintext generation can read account backups
   and WAL, including framework data in backups or WAL written before the
   cutover. See
   [framework access](frameworks-access.md#licensed-material-and-where-it-may-be).
   Delete with owner approval once the new repository proves PITR at the current
   30-day boundary and recent recovery, and any hold clears. Check live bucket
   versioning and verify no objects, older versions or multipart uploads remain
   before removing policy and bucket. Deletion cannot recall existing copies.
   [OCI object versioning][oci-versioning]

## Archiving outage

Restore bucket access first. WAL queues on failure. A dual-archive failure can
abort to A through the recorded overlap transition. After the record commits B
alone, fix B or move to a new generation. If recovery starts a new timeline from
a frozen repository or the current writer cannot safely accept any more WAL, use
the [restore ingress hold](backups.md#restore-ingress-hold) and record the
approved replacement as the sole writer before enabling archiving. Retire the
previous writer in the same write, preserve the floor and record the recovery
boundary or gap.

If disk is near full and the owner accepts a PITR gap, hold the updater and
backup jobs and install an owner-controlled Quadlet override that replaces the
archive command argument with `-c archive_command=/bin/true`. Run the user
manager's `daemon-reload` and recreate PostgreSQL. Verify `SHOW archive_command`
and that the `.ready` archive queue drains. Alert throughout the override and
record the gap. PostgreSQL's `-c` setting takes precedence over `ALTER SYSTEM`.
When bucket access returns, remove the override, reload and recreate PostgreSQL,
then verify `SHOW archive_command` again. Require `check`, the WAL queue to
drain and a full before resuming scheduled backups and claiming new PITR
coverage. Rehearse both overrides and checks on synthetic data. [PostgreSQL
configuration precedence][postgres-settings]

## Key loss and rotation

If Vault or the pinned version is unavailable, use offline escrow. Prove restore
before pinning a replacement Vault record for the same key. If both copies and
the host copy are lost, preserve the ciphertext in case the key is recovered.
Report the RPO gap and start a fresh encrypted full from the live cluster if
available.

Rotate S3 credentials independently. Replace the repository to rotate the
passphrase on disclosure or custody change. Keep both custodied key copies until
the owner verifies repository deletion and clears any hold. Revoke exposed
bucket access immediately and stop writes to an exposed repository if overlap
would extend the exposure. Record any PITR gap. A holder of an old key and
copied ciphertext can still read that copy.

If a container or host may be the source of disclosure, isolate it and revoke
its bucket credentials and Vault access. Rebuild a trusted environment and
recover from a verified source before issuing replacements there. Treat data and
backups writable by the compromised environment as suspect.

## Monitoring, cost and release order

Identify the active bucket in backup age, outcome, expiry, WAL health and
restore proof. Track WAL success separately for each bucket during overlap. Keep
the current age and WAL thresholds, and alert for active failures, unknown
results or an overdue legacy cleanup without an explicit hold. Keep credentials
and row data out of results.

Budget two repositories for cutover or rotation. An extra 40 to 70 GB for a
month costs about USD 1.02 to 1.79 before additional PUTs and proof GETs. Check
free disk for the restore, its WAL and the live backlog. Measure backup and
restore duration, WAL lag and disk use on A1, and update
[host budgets](budgets-host.md). Use the existing Vault allowance.
[Size estimate](backups.md#size) · [OCI storage pricing][oci-price] · [OCI Vault
allowance][oci-free]

1. Agree the ADR change and independently review host and IAM changes. Ship a
   signed preparatory bundle with pgBackRest 2.60.1, forward-only enforcement
   and pinned secret reconciliation while the previous repository is the sole
   writer. Verify the deployed binary and pre-stage secrets before the mounts
   ship. Use green CI on each exact release commit. [PGDG package index][pgdg]
2. Initialize the new repository, deploy and prove dual archiving, persist the
   forward-only mark and activate the new-only bundle. Verify the jobs, then
   freeze and retire the previous writer as above.

**Owner approval required.** Amend ADR 0006 to accept client-side encryption,
separate buckets and custody in regional Vault and offline escrow. Approve the
forward-only cutover, 30-day minimum read-only retention with residual plaintext
exposure, a fresh encrypted writer after recovery from a frozen source, and
later deletion of each retired repository and its passphrase.

[cipher-type]:
  https://pgbackrest.org/configuration.html#section-repository/option-repo-cipher-type
[cipher-pass]:
  https://pgbackrest.org/configuration.html#section-repository/option-repo-cipher-pass
[cipher-setup]:
  https://pgbackrest.org/user-guide.html#quickstart/configure-encryption
[stanza-source]:
  https://github.com/pgbackrest/pgbackrest/blob/release/2.60.1/src/command/stanza/create.c
[backup-source]:
  https://github.com/pgbackrest/pgbackrest/blob/release/2.60.1/src/command/backup/backup.c
[wal-source]:
  https://github.com/pgbackrest/pgbackrest/blob/release/2.60.1/src/command/archive/push/file.c
[repo-list]: https://pgbackrest.org/command.html#command-repo-ls
[spool]:
  https://pgbackrest.org/configuration.html#section-general/option-spool-path
[commands]: https://pgbackrest.org/command.html
[archive-push]: https://pgbackrest.org/command.html#command-archive-push
[pgbackrest-release]: https://pgbackrest.org/release.html#2.60.1
[pgdg]: https://apt.postgresql.org/pub/repos/apt/pool/main/p/pgbackrest/
[postgres-settings]: https://www.postgresql.org/docs/18/config-setting.html
[oci-encryption]:
  https://docs.oracle.com/en-us/iaas/Content/Object/Tasks/encryption.htm
[oci-object-iam]:
  https://docs.oracle.com/en-us/iaas/Content/Identity/Reference/objectstoragepolicyreference.htm
[oci-secrets]:
  https://docs.oracle.com/en-us/iaas/tools/python/latest/api/secrets/client/oci.secrets.SecretsClient.html#oci.secrets.SecretsClient.get_secret_bundle
[oci-versioning]:
  https://docs.oracle.com/en-us/iaas/Content/Object/Tasks/usingversioning.htm
[oci-price]: https://www.oracle.com/cloud/storage/archive-storage/faq/
[oci-free]:
  https://docs.oracle.com/en-us/iaas/Content/FreeTier/freetier_topic-Always_Free_Resources.htm
[openssl-rand]: https://docs.openssl.org/3.5/man1/openssl-rand/
[podman-secret]:
  https://docs.podman.io/en/v5.8.2/markdown/podman-secret-create.1.html
[podman-run]:
  https://docs.podman.io/en/v5.8.2/markdown/podman-run.1.html#secret-secret-opt-opt
[podman-secret-rm]:
  https://docs.podman.io/en/v5.8.2/markdown/podman-secret-rm.1.html
