# Backups with pgBackRest

Proposed. How the [backup rules](deployment.md#backups) run on the host:
pgBackRest to the private backup bucket over the
[Object Storage](deployment.md#object-storage) S3 endpoint, continuous WAL,
weekly full, daily differential, weekly verify, 30-day retention, a monthly auth
restore check, health results and a production restore procedure. See the
[client-side encryption proposal](backups-encryption.md) for the repository
migration and recovery changes.

## Version and image

The current image pins pgBackRest 2.60.0 (released 2026-10-18). PostgreSQL 18
support arrived in 2.55.0
([release notes](https://pgbackrest.org/release.html)). The project announced
the end of maintenance on 2026-04-27 and its continuation with sponsors on
2026-05-18 ([news](https://pgbackrest.org/news.html)); that risk was weighed and
accepted, and a later release goes through the same review.

`archive_command` runs inside the PostgreSQL container, so the binary must be
there; the upstream pgvector image has none. The Images workflow builds
`trachuan-postgres` from the pinned pgvector digest (Debian 13, PostgreSQL 18.7)
plus the PGDG package `pgbackrest=2.60.0-1.pgdg13+1` (arm64 `.deb` SHA-256
`ac2348d57b62f8ae02bb012d7b776c7aff10aa9d798b03dcde0e05ad6f53ae4e`, checked
before install) and Debian's `ca-certificates`, which pgBackRest needs to verify
the Object Storage TLS chain (`--no-install-recommends` skips it). The image
keeps user 999 and the upstream entrypoint, passes the workflow's binary-list
check, and is pinned by digest and attested like the other images.

## Configuration and secrets

Static options live in `/etc/pgbackrest/pgbackrest.conf` in the host bundle:
stanza `trachuan`, `pg1-path` and `pg1-socket-path` for the live cluster;
`repo1-type=s3`, `repo1-s3-endpoint`
`axj58m4cpgbr.compat.objectstorage.ap-singapore-1.oraclecloud.com`,
`repo1-s3-region=ap-singapore-1`, `repo1-s3-bucket=trachuan-backups`,
`repo1-s3-uri-style=path`, `repo1-s3-key-type=shared`,
`repo1-path=/pgbackrest/trachuan`, `repo1-cipher-type=none`,
`repo1-retention-full-type=time`, `repo1-retention-full=30`,
`compress-type=zst`, `archive-async=y` with `spool-path` on
`/srv/trachuan/data/pgbackrest`, `process-max=2`, `start-fast=y`, TLS
verification on.

The PostgreSQL container runs with `RunInit=true`, so Podman's init is PID 1.
Asynchronous `archive-push` leaves a background process; with PostgreSQL as PID
1 it was adopted as an untracked child and its exit restarted every backend
(seen on the host on 2026-11-14).

No credential enters that file, an image, an environment value or a command
line. The PostgreSQL unit mounts the Podman secrets `backup_s3_key`,
`backup_s3_key_secret` and `postgres_password`. Before PostgreSQL starts, a
wrapper writes `repo1-s3-key` and `repo1-s3-key-secret` to
`/run/pgbackrest/conf.d/secret.conf` and a password file to
`/run/pgbackrest/pgpass`, both on a private tmpfs, owner 999, mode 0600; the
container environment carries only the paths
(`PGBACKREST_CONFIG_INCLUDE_PATH=/run/pgbackrest/conf.d`, `PGPASSFILE`), so
`podman exec` sees them and connections keep SCRAM. A missing or empty secret
stops the container.

## WAL archiving and its failure

PostgreSQL runs with `archive_mode=on`,
`archive_command='pgbackrest --stanza=trachuan archive-push %p'` and
`archive_timeout=300` (RPO minutes). `archive-push-queue-max` stays unset: when
the bucket or credentials fail, WAL accumulates in `pg_wal` on the data volume
rather than being dropped, and PostgreSQL stops only if that volume fills. The
hourly check reads `pg_stat_archiver` and the size of `pg_wal`: archiving
failing for more than 30 minutes, or `pg_wal` above 10 GB, is unhealthy. Restore
bucket access first. The Quadlet passes `archive_command` with `postgres -c`.
The existing runbook's `ALTER SYSTEM` example cannot override that setting. The
[encryption proposal](backups-encryption.md#archiving-outage) specifies the
effective override and recovery checks.

## Schedules and results

Host user units run `podman exec trachuan-postgres pgbackrest …`, so backups use
the archiving binary, configuration and data directory. All share one lock.
Health tries it once and records `skipped` when it is busy. Every other unit
waits for the lock within its own deadline (the time spent waiting counts); if
the deadline passes while it waits, it records `failed` with the reason that the
lock was not acquired and exits non-zero, so a skipped full or differential is
never silent. Health runs at minute 30, so its calendar never meets another
unit's; after downtime, `Persistent=true` can still start several overdue units
together, and the waiting rule above orders them.

| Unit               | Schedule (Asia/Ho_Chi_Minh)  | Command                                  | Result file          | Deadline   |
| ------------------ | ---------------------------- | ---------------------------------------- | -------------------- | ---------- |
| Full               | Sunday 02:00                 | `backup --type=full`                     | `full.json`          | 4 hours    |
| Differential       | Monday to Saturday 02:00     | `backup --type=diff`                     | `diff.json`          | 2 hours    |
| Verify             | Wednesday 04:00              | `verify --output=text --verbose`         | `verify.json`        | 4 hours    |
| Health             | hourly at minute 30          | `info --output=json`, `pg_stat_archiver` | `health.json`        | 5 minutes  |
| Auth restore check | first day of the month 05:00 | below                                    | `restore-check.json` | 20 minutes |

Timers use `Persistent=true`; a unit that passes its deadline is stopped and
records `failed`. Expiry runs after every successful backup; differentials
remain until their full expires. `stanza-create` and `check` run once from the
runbook. After each corpus release activation the runbook takes a full backup,
so later differentials do not each carry the new corpus database.

Each unit keeps one JSON file under `/srv/trachuan/state/backup/`, written
atomically, and logs one journald line per change. Each unit also appends its
final result as one JSON line (`kind`, `status`, `reason`, `started_at`,
`finished_at`, `last_success_at`; no secrets) to `/var/log/trachuan/backup.log`,
which the host job log path ships to OCI Logging and `logrotate-trachuan`
rotates. The file holds the last attempt (`started_at`, `finished_at` or null,
`status`, `reason`) and `last_success_at`. The unit writes `running` with
`started_at` before it starts work and its final status when it ends. Statuses:
`ok`; `failed` (it ran and found a problem, or its command failed); `unknown`
(storage unreachable, output malformed or a timestamp invalid; never treated as
ok); `skipped` (health only: another unit held the lock). A backup that
pgBackRest records with an error, including page checksum errors, is `failed`
and does not count as the newest good backup. Verify is `ok` only when its
verbose text output reports status ok for the stanza and no invalid files.
Health is `failed` when the newest good backup is older than 26 hours or
archiving is unhealthy as above. A full backup took 8.5 minutes on 2026-11-15
(8.4 GB database, 4.3 GB in the repository), so 26 hours leaves ample room for
the Sunday full after Saturday's differential.

For alarms: no file means the unit has never run; `running` past the unit's
deadline means it crashed or hung; a last `started_at` older than twice the
unit's period means it stopped running; `failed`, `unknown` and repeated
`skipped` mean it ran without success, and `last_success_at` tells how long ago
it last succeeded. This note adds no alarm transport.

## Auth restore check

`restore-check.sh` proves that the newest backup restores the auth database. It
is not a recovery-time measurement. It takes the backup lock, removes any
leftover `trachuan-restore-check` container and directory first, then runs that
container from the same image with only `/srv/trachuan/data/restore-check`
mounted as its data directory, its own socket directory and spool, no TCP
listener (`listen_addresses=''`), the repository keys as its only secrets,
`archive_mode=off`, 2 GB memory and one CPU. It starts PostgreSQL with its own
`hba_file` that allows only `local` peer authentication on that private socket,
for the `postgres` role and, through a peer map, for `auth_owner` on `auth` (the
container runs as user 999, the image's `postgres`), so it needs no database
password and the restored SCRAM rules never apply to it. A tampered backup
supplies its own configuration, so every setting that runs a command or loads a
library is forced on the command line and the data directory is mounted
`noexec`. This narrows the risk but cannot close it: a backup also supplies the
catalogs, so write access to the repository remains equivalent to holding its
keys. It restores with
`--set=<newest good backup label> --db-include=auth --type=immediate --target-action=promote`
(other databases are restored as sparse zeroed files and never opened), waits up
to 20 minutes for recovery to end and `pg_is_in_recovery()` to return false,
runs smoke queries as `auth_owner`, never as a superuser beside the repository
keys ([database roles](system.md#database-roles)) (auth schema version; row
counts of accounts, sessions, grants and tokens; one index read), records the
elapsed time and result, and always stops the container and deletes the
directory through a trap on exit and signals. It never touches the live cluster,
its socket or archive.

## Production restore

Use the [backup runbook](../runbooks/backups.md#restore-production) to restore
and promote the chosen target. Run migrations, then commit the auth restore
fence. The fence increments every `auth_epoch`, revokes sessions and tokens and
revokes every live framework entitlement with reason `restore`. Start the
services for loopback checks, prewarm the active corpus database and pass
readiness before reopening ingress. Record the timed whole-cluster rehearsal
before claiming the one-hour RTO. [Auth](auth.md) ·
[Framework entitlements](frameworks-access.md#classes-and-entitlements) ·
[Corpus prewarm](../runbooks/corpus-restore-manual.md#prewarm-a-corpus-database)

### Restore ingress hold

Before replacing production data, the restore operator takes the backup lock,
atomically writes `/srv/trachuan/state/restore-hold`, and syncs the file and
parent directory to disk. The root-owned hold records the restore attempt and
lives outside the restored data directory. Stop serving listeners and confirm
in-flight requests have drained before touching data. Only operator-controlled
loopback recovery and readiness traffic can run while the hold exists.

Caddy and every other listener that can serve restored data or accept restored
credentials must check the hold before startup, including after reboot. The
updater defers deploy, secret and corpus activations and leaves this hold in
place on every path, including rollback and recovery. Its existing `deploy-hold`
has a separate lifecycle. Require a release with the restore hold guards, and a
rollback floor that preserves them, before replacing data. If a listener or
updater cannot check the hold, it keeps ingress closed.

Only the restore operator may clear the hold after confirming the fence
committed for this restore attempt and loopback readiness passed. A failure,
interruption or reboot leaves the hold in place. If completion is uncertain,
rerun the fence and readiness under the hold. On a synthetic deployment, reboot
immediately after promotion and before the fence commits. Attempt a listener
start and updater run, and verify a session and entitlement restored from before
their revocation cannot be served. Complete the fence and readiness under the
hold, clear it as the operator, and verify ingress opens with both still
revoked.

## Size

The cluster backup includes the corpus databases (4.0 GB for vn/20261114_3, plus
the retained release), and restoring a release archives several GB of WAL.
Time-based retention of 30 days keeps five or six weekly fulls (one older than
30 days anchors the window) with their differentials and WAL. At roughly 4 to 6
GB per compressed full that is about 25 to 36 GB of fulls; differentials and WAL
add more, more again in a week with a release activation before its full. Expect
40 to 70 GB, above the 50 GB Object Storage estimate; record the measured size
in [host budgets](budgets-host.md) after the first month (0.0255 USD per
GB-month above the free amount). Moving corpus databases to a separate
unarchived cluster is the alternative if growth demands it.
