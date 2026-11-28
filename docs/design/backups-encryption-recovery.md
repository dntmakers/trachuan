# Encrypted backup recovery

Proposed recovery design for
[client-side backup encryption](backups-encryption.md). The
[backup runbook](../runbooks/backups.md) owns the procedure after approval.

## Select a recovery source

Choose the bucket, path, stanza, cipher, backup label, target and timeline from
the recovery inventory. A frozen or active repository can serve a target within
its recorded coverage. Supply that repository's config and bucket-scoped
read-only credentials to both `restore` and the PostgreSQL `restore_command`.
For `aes-256-cbc`, supply the exact Vault passphrase version or matching escrow
copy. For the plaintext generation, set `repo1-cipher-type=none` and use only
the bucket read credentials. Leave `repo-cipher-pass` unset and omit its secret
mount. Keep the selected secret include through replay. Require both the backup
and WAL in the selected repository. Stop if readability or coverage cannot be
established. [pgBackRest restore][restore] · [Repository cipher options][cipher]
· [PostgreSQL continuous archiving][postgres-pitr]

Every recovery attempt gets its own empty spool with a unique path. Mount it at
the same path and set `PGBACKREST_SPOOL_PATH` for base restore and replay, even
if separate containers run them. Keep it private to UID 999 and remove it after
replay or on abandonment. Never mount the production writer spool in recovery.
pgBackRest clears only the stanza archive spool it sees during `restore`.
[pgBackRest spool option][spool] · [Pinned restore code][restore-source]

## Promote and resume archiving

Follow the [restore ingress hold](backups.md#restore-ingress-hold) for a
production restore. Keep scratch recovery isolated with `archive_mode=off`. In
production, use `archive_mode=on` with `archive_command` and `archive_library`
empty until the writer is ready, so PostgreSQL queues WAL after promotion.
Prepare a fresh writer repository for recovery from a frozen source. A timeline
chosen against a frozen archive can conflict with one already in the active
archive. Preserve old repositories for their timelines. For recovery from the
current writer, check the promoted timeline against that repository's history
before resuming writes. Enforce the
[forward-only writer mark](backups-encryption.md#cutover-and-retention) when
changing writers. Enable encrypted archiving and run `check`, then run
migrations and the restore fence, take the promoted cluster's first full, and
follow the [production readiness steps](backups.md#production-restore) before
reopening traffic. [PostgreSQL archiving settings][postgres-archive-settings] ·
[PostgreSQL timelines][postgres-timelines] · [pgBackRest WAL collision
checks][wal-source]

## Restore proof

- Rehearse whole-cluster PITR before making the cutover forward-only. Use the
  pinned image, a private host scratch directory and socket, and a target after
  the selected full. Keep production mounts and application ingress out of the
  scratch restore. Recover the key from offline escrow, and repeat with Vault in
  a fresh container. Force a WAL switch after the target and prove `archive-get`
  uses the selected repository and PostgreSQL promotes at the target. Test the
  previous repository near the boundary from its recovery config. Prove one
  restore and WAL replay from the legacy plaintext bucket using read-only S3
  credentials without a `repo-cipher-pass` setting or secret mount. Run
  migrations, the auth restore fence and readiness in isolation. Remove restored
  data and the spool on every exit.
- With read-only S3 credentials, confirm raw encrypted metadata can be read,
  then prove pgBackRest cannot decrypt with a missing or incorrect passphrase.
  Require startup to fail when the encrypted secret mount is missing. Check both
  exit codes and the stanza status in `info` output.
- Use a synthetic cluster for exact PITR and post-promotion write proofs. Put
  distinct transactions before and after a named restore point. Seed the
  previous recovery's spool with invalid bytes under the exact segment name
  needed for the new recovery. Require `archive-get` to fetch that segment from
  the chosen bucket and verify which transactions survive. Prove `check` and a
  full after promotion from a frozen source, using a separately scoped writer in
  a private test bucket containing only synthetic data.
- Run the monthly auth restore with a distinct read-only S3 identity scoped to
  the active bucket. Its credentials allow list and read and exclude creation,
  overwrite and deletion. The container mounts only that pair and the matching
  passphrase. Verify the IAM separation with the same permission set on a
  synthetic repository. [OCI Object Storage permissions][oci-object-iam]
- Restore the first encrypted backup whose contents include the activated
  frameworks database, whether activation happens before or after the backup
  cutover. Check its schema and an index in isolation. Record only repository
  ID, backup label, target, timeline, stage durations, WAL lag and pass/fail.
  Include key recovery and readiness in the whole-cluster elapsed time before
  claiming the one-hour RTO. Keep account and framework row data out of
  evidence.

[restore]: https://pgbackrest.org/command.html#command-restore
[cipher]:
  https://pgbackrest.org/configuration.html#section-repository/option-repo-cipher-type
[spool]:
  https://pgbackrest.org/configuration.html#section-general/option-spool-path
[restore-source]:
  https://github.com/pgbackrest/pgbackrest/blob/release/2.60.1/src/command/restore/restore.c#L49-L52
[postgres-pitr]:
  https://www.postgresql.org/docs/18/continuous-archiving.html#BACKUP-PITR-RECOVERY
[postgres-timelines]:
  https://www.postgresql.org/docs/18/continuous-archiving.html#BACKUP-TIMELINES
[postgres-archive-settings]:
  https://www.postgresql.org/docs/18/runtime-config-wal.html#GUC-ARCHIVE-MODE
[wal-source]:
  https://github.com/pgbackrest/pgbackrest/blob/release/2.60.1/src/command/archive/push/file.c
[oci-object-iam]:
  https://docs.oracle.com/en-us/iaas/Content/Identity/Reference/objectstoragepolicyreference.htm
