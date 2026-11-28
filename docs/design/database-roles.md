# Database roles

Proposed. Give law and frameworks separate restore owners. Move `serve` to a
DML-only auth login. Preserve entitlement history and release pointers when the
server container is compromised. Keep the signature, catalog and access checks
in [law activation](corpus-release-activation.md),
[frameworks restore](frameworks-release-restore.md) and
[frameworks access](frameworks-access.md).

## Role and secret contract

| PostgreSQL role        | Scope                                                                                                                                     | OCI Vault and Podman secret                                     |
| ---------------------- | ----------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------- |
| `activator`            | Own law release databases and their restored objects                                                                                      | `activator_db_password`                                         |
| `frameworks_activator` | Own frameworks release databases and their restored objects                                                                               | `frameworks_activator_db_password`                              |
| `auth_owner`           | Own `auth`, its application schema, tables, routines and Goose version table. Run migrations, host operations and administrative commands | `auth_db_password`, derived `auth_owner_database_url` for Go    |
| `auth_runtime`         | Run `serve` queries in `auth` with the explicit grants below                                                                              | `auth_runtime_db_password`, derived `auth_runtime_database_url` |
| `corpus_reader`        | Connect to published releases and read granted data for `serve` and smoke tests                                                           | `corpus_reader_password`, derived `server_corpus_database_url`  |
| `postgres`             | Provision roles and prepare empty release databases                                                                                       | `postgres_password`                                             |

Each non-superuser login has `NOCREATEDB`, `NOCREATEROLE`, `NOREPLICATION` and
`NOBYPASSRLS`, with zero explicit role memberships or parameter grants. Check
predefined roles as well. The database owner has implicit `pg_database_owner`
rights in its own database. [Predefined roles][predefined]. PostgreSQL grants
`CREATEDB` cluster-wide and requires it for a non-superuser owner to rename a
database or change ownership. Object ownership controls alteration and deletion.
[Role attributes][role-attributes], [ALTER DATABASE][alter-database],
[GRANT][grant].

Revoke `PUBLIC` database privileges including `TEMP` in `auth`, `postgres` and
releases. Grant the restore roles `CONNECT` on `postgres`. Grant `auth_runtime`
`CONNECT` on `auth` and `USAGE` on `public`. Revoke public schema creation and
execution of application routines, including default `EXECUTE` for new
`auth_owner` routines. [Privileges][privileges], [default
privileges][default-privileges].

Use first-match `pg_hba.conf` rules on the Unix socket and TCP loopback. Allow
auth roles only in `auth`, each restore role in `postgres` and its kind's
release name pattern, and readers in either release pattern. Require SCRAM, then
reject each named login before any wider rule. Use the same policy for bootstrap
and repair. Replace HBA atomically, reload and prove the routes. [Host
authentication][hba].

Add both Vault names to `deploy/oci/iam.tf`, `deploy-policy.json` and
`deploy_vault.py`. Reconcile each password and percent-encoded URL from the same
Vault version under the deploy lock. Use short-lived Podman clients with only
their required secrets. Give archive input only to restore clients. Keep
passwords out of arguments, output and logs. Publish a reconciled version after
SQL and clients accept it. Recreate consumers when a Podman secret changes. [OCI
IAM][oci], [Podman secret replacement][podman].

Every live password-bearing client requires SCRAM. Set
`require_auth=scram-sha-256` in derived URLs and enforce it in production pgx
config, including migrations and admin commands. Set
`PGREQUIREAUTH=scram-sha-256` for libpq helpers and probes. Pin the endpoint and
database. Peer recovery carries no password. [libpq][libpq], [pgx][pgx].

### Auth grants

| Objects in `auth`                                                                       | `auth_runtime` privileges                                                                                                                                                    |
| --------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `corpus_release`, `frameworks_release`                                                  | `SELECT` for serving, including public summary metadata                                                                                                                      |
| `framework_entitlement`                                                                 | Column `SELECT` on `email`, `scope`, `revoked_at`. Return `scope` only and order by `scope`                                                                                  |
| `framework_entitlement_event`, `release_journal`, `retired_release`, `goose_db_version` | None, including their sequences                                                                                                                                              |
| Account, OAuth, session, credential, mail and issue report relations                    | Explicit per-table `SELECT`, `INSERT`, `UPDATE` and `DELETE` only where serving, mail or retention uses each verb. `issue_report_export` gets read and retention delete only |
| Sequences and routines                                                                  | `USAGE` for sequences called by serving SQL, including `mail_outbox_id_seq`. `EXECUTE` for reviewed runtime calls only. Keep trigger routines invoker-rights                 |

Build and test the grant list from `apps/server/sql`, transaction code and
background workers. Include privileges required for `RETURNING`, row locking,
upserts, foreign key cascades and deferred credential checks. Withhold `CREATE`,
`TRIGGER`, `TRUNCATE`, `MAINTAIN`, dangerous parameter settings and all
`SECURITY DEFINER` entry points with owner rights. Keep `auth_owner` as the
existing owner. Inspect ownership, objects, settings and ACLs against the schema
from the pinned migrations before accepting a host. Unexpected catalog state
requires incident review before running owner SQL over application data.

The server Quadlet injects `auth_runtime_database_url` as
`SERVER_AUTH_DATABASE_URL`. The migration Quadlet and isolated `server admin`
invocations inject `auth_owner_database_url` under the same variable name.
`apps/server/internal/migrate` verifies the expected owner before applying Goose
migrations in production. Each future migration that adds a serving query adds
its grant in the same transaction. Apply the initial grants with idempotent,
signed host SQL after validating the Goose version. This keeps the schema
version stable across the cutover and allows the image updater to use its
existing version check on rollback. Bootstrap runs the same grant check before
ingress opens. Retire the previously shared `server_auth_database_url` after the
credential fence.

Keep pointer writes, entitlement administration and `admin restore-fence` on
`auth_owner`, including `auth-psql.sh`. The
[monthly restore check](backups.md#auth-restore-check) keeps its `auth_owner`
peer mapping and checks the contract for the backup date. For
[production PITR](backups.md#production-restore), hold ingress and release
phases. Start with TCP off, a private socket and trusted external HBA and ident
files. Permit `postgres` through peer authentication and use the recovery
startup safeguards from the monthly check. Through peer, reconcile current Vault
passwords, role attributes, memberships, ownership and grants. Old backups can
restore an obsolete `postgres` password and HBA. Install the current HBA before
enabling TCP. Verify SCRAM and role restrictions. Follow the
[recovery release and readiness gates](backups.md#production-restore). [Peer
authentication][peer].

## Restore and host activation

Keep the host's deploy lock and the same advisory key in `auth`. A dedicated
`auth_owner` session holds the advisory lock using only catalog calls. The host
serializes both kinds and checks the lock before each mutation. The updater
reads and finishes the journal in other bounded owner sessions. Remove
`activator` access to `auth` after legacy updaters are fenced. A runtime session
can still hold a conflicting advisory or table lock. The phase waits, raises its
integrity alarm and keeps the manifest pending. [Advisory locks][locks].

One kind-to-role mapping controls target names, credentials, catalog owner
checks, retained adoption, prewarm, restore, grants, abort and prune. Match the
name, expected owner and recorded database OID before each guarded step. The
host always reads both kinds' protections, even when a kind is disabled. Cleanup
terminates only the selected restore role's leftover sessions and removes its
unprotected targets from `postgres`. Wrong owners, unexpected names, mismatched
OIDs or a failed protection read wait and alert. A separate one-time legacy
sweep uses the existing drop guard and host record.
[Activation record](activation-record.md).

New targets follow this order.

1. From `postgres`, validate the signed kind and name and confirm the name is
   free after guarded cleanup. Journal intent, create from `template0` with
   connections disabled, record the OID and revoke `PUBLIC` access. Creation is
   outside the transaction. On a crash before recording the OID, clean up only a
   matching, unprotected, `postgres`-owned database with connections still
   disabled and no evidence of handoff. Otherwise wait for the owner. [CREATE
   DATABASE][create-database].
2. Enable connections while access is private to `postgres`, verify identity,
   prepare extensions and apply the large object ACL below. Set the framework
   log defaults for the database, `frameworks_activator` and `corpus_reader`.
   Close privileged target sessions. From `postgres`, hand off ownership as the
   final privileged action on the target and persist the handoff. Recovery uses
   the restore identity or replaces an unprotected target from `postgres`.
3. Restore into the host-created target with `--create` off and
   `--no-owner --no-privileges --no-comments`. Generate `--use-list` from
   `pg_restore --list` with `--create` off. Keep only `public` schema, table,
   table data, sequence and sequence data, defaults, constraints, indexes and
   their attachments, statistics and archive session entries. Omit host-managed
   extensions and image-managed executable entries. Refuse other selectable
   entries, including shared-object comments, ACLs and labels. Custom dumps
   carry `DATABASE` archive metadata. With `--create` off, PostgreSQL skips
   `DATABASE` and `DATABASE PROPERTIES` in listing and restore. Exclude both if
   visible. Apply image SQL as the restore owner, then check catalogs, reader
   grants, release row and smoke. Guard reuse by owner and OID. Law prewarm uses
   `activator` on its own releases. Avoid ownership sweeps across both kinds.
   [pg_restore][pg-restore], [archive rules][archive].

Restore validators check for any target database comment in `pg_shdescription`
by `classoid = 'pg_catalog.pg_database'::regclass` and recorded database OID.
Check after restore and before retained adoption, smoke or publication. Check
for unexpected shared security labels on the same identity. On a match, halt and
alert without logging the contents. Quarantine the target and clear the shared
value from `postgres` or drop the candidate under the normal guard.
[COMMENT][comment], [shared descriptions][descriptions].

In `pg_db_role_setting`, always validate database defaults (`setrole = 0`).
Validate per-login rows only for logins allowed to enter the target by the
verified host HBA policy, including `postgres` during preparation. Apply the
same filter to both snapshots of the frameworks check. Law expects no such rows.
Frameworks expects exactly the four log settings for the database, its restore
login and `corpus_reader`. Check the login-wide defaults and parameter ACLs
separately, and reset unexpected restore-role defaults from `postgres`. A login
can set its own defaults for a database it cannot enter. Those rows cannot
affect permitted sessions or fail the other kind's retained check. [ALTER
ROLE][alter-role], [role settings catalog][role-settings].

### Large objects

In `auth`, `postgres` and release databases, revoke `PUBLIC` and direct client
`EXECUTE` on every built-in large object creation or import function, including
all `lo_creat`, `lo_create`, `lo_from_bytea` and `lo_import` signatures. Check
that the non-owner logins in `auth` and all non-superuser logins in `postgres`
and releases lack effective access. Check `pg_largeobject_metadata` for any
objects before publishing or admitting those databases to this contract. In
`auth`, apply the ACL once as `postgres` during cutover after fencing legacy
sessions. Use catalog-only SQL, a pinned catalog search path, disabled event
triggers and cleared preload settings. Replace legacy release databases through
normal signed activation and retirement before cutover. Prepare their
replacements before loading any dump. [Large objects][large-objects].

## Attacker traces

- **Compromised server container.** The process can read its runtime URL, corpus
  reader URL and serving keys. Keep owner secrets and the Podman socket out of
  the container. Keep the host's [metadata block](deployment.md#secrets).
  PostgreSQL authenticates runtime only to `auth`. The login has zero grants on
  events and cannot grant itself access, disable their trigger, change the
  entitlement inputs, or replace pointers, routines or tables. Cascades from
  accounts do not reach entitlements or their events. The process can still
  misuse auth DML, impersonate callers and read or disclose content available to
  the serving process. Keep role separation as a history and release integrity
  control and handle container compromise as an incident.
- **Hostile signed dump, either kind.** The restore login can disrupt databases
  it owns. PostgreSQL denies creation, rename, switching to another owner and
  changing the other kind's database defaults. HBA denies cross-kind and auth
  connections. SQL inside an allowed entry can write a cluster-readable comment
  before the catalog check. The check cannot undo disclosure. The signature
  remains the boundary. Both kinds share CPU, connections, disk and WAL.

## Maintenance cutover and rollback

Use the existing ingress hold, stop and readiness checks under both locks.
Schedule a short outage and serve from one process so credential revocation
keeps its [cache contract](web-api.md#sessions). Defer unrelated rotations.

1. **Image release A, compatibility.** Create the new Vault entries and grant
   host IAM access. Ship an updater and server that support both role maps,
   enforce SCRAM and initialize a durable `legacy` marker outside the bundle.
   Keep the legacy role map. Provision new logins and derived secrets. Apply
   large object and shared catalog safeguards to new corpus targets. Test A
   against the split map on a restored test host.
2. **Image release B, floor.** Set `release_floor` to A's sequence in a
   compatible bundle. Complete B with the old role map. Verify its active state,
   stored floor and rejection of older forward, rollback and recovery
   candidates. The updater commits the floor only after successful apply. Allow
   only bundles whose updater, migrations, Quadlets and secrets support the new
   marker and schema. [Release floor](release-floor.md).
3. **Image release C, cutover.** Require B's committed floor, zero open corpus
   operations and proof that every served or retained law target meets the large
   object policy. Stage a new `auth_db_password` version as `PENDING`. Sweep
   unprotected legacy targets under the drop guard. A protected frameworks
   target needs owner-approved conversion. Close ingress, stop the server and
   hold release phases. Record `split` and the exact Vault version durably
   before changing privileges or passwords. Read state before secret sync and
   owner sessions, including recovery's schema read.
4. The operator promotes that version to `CURRENT`. The host has read-only Vault
   access. On every attempt, including after reboot, fetch the exact version
   recorded in `split` and independently verify it is `CURRENT`. Missing state
   or a mismatch holds ingress and stops before owner login. From `postgres`,
   disable `auth_owner` login, end legacy sessions, recheck catalogs, apply ACLs
   and set the staged SCRAM password. Re-enable login only after verifying the
   version still matches. End old restore sessions, remove law `CREATEDB` and
   `auth` access, and reload HBA. Remove the shared URL, prove new sessions and
   old password refusal, and pass runtime readiness before reopening ingress.
   [OCI promotion][promote], [OCI IAM][oci].

Before `split`, recover with the legacy map and the committed floor. After
`split`, preserve roles, passwords, grants, HBA and release state on rollback to
A or B. Record completion durably and use the `CURRENT` gate on every incomplete
cutover retry. Reopen only after split-map readiness passes. Prove both rollback
paths before publishing the first signed frameworks dump.

## Proofs and approval

- In PostgreSQL 18, test both restore roles against the other kind's active and
  retained targets. Verify denied connection, create, rename, drop and owner
  changes. In each direction, set the attacking login's own defaults in the
  other kind's database and verify the retained check passes. Change a permitted
  login's setting or a database default and verify refusal.
- Restore a synthetic archive made by the real law publisher
  (`apps/corpus/internal/stage/build/dump.go`). Show `DATABASE` in a list made
  with `--create`, then prove the restore with `--create` off executes only the
  allowlist. Detect a synthetic shared comment after restore and on retained
  adoption. Check cleanup, OID, log settings and prune. Prove large object
  creation fails for runtime, reader and both restore roles.
- Exercise serving, migrations, entitlement administration and the restore fence
  with their real roles. Run the scope query with exactly the three column
  grants. Verify audit and pointer writes and DDL are denied to runtime. Verify
  password-bearing pgx and libpq clients succeed with SCRAM and refuse
  cleartext, MD5 and unauthenticated endpoints before sending a credential. Use
  synthetic passwords.
- Rehearse pre-split PITR, new-name activation and a missing verified artifact.
  Crash at each cutover step, including before Vault promotion and after SQL
  rotation. Check wrong `CURRENT`, both eligible rollbacks, the prior floor
  commit, single-process serving and revoked credentials on reopening.
- **Owner approval.** Approve the maintenance window after rehearsal and the
  one-time catalog-only `postgres` session in existing `auth`.

[role-attributes]: https://www.postgresql.org/docs/18/role-attributes.html
[predefined]: https://www.postgresql.org/docs/18/predefined-roles.html
[alter-database]: https://www.postgresql.org/docs/18/sql-alterdatabase.html
[alter-role]: https://www.postgresql.org/docs/18/sql-alterrole.html
[create-database]: https://www.postgresql.org/docs/18/sql-createdatabase.html
[grant]: https://www.postgresql.org/docs/18/sql-grant.html
[privileges]: https://www.postgresql.org/docs/18/ddl-priv.html
[default-privileges]:
  https://www.postgresql.org/docs/18/sql-alterdefaultprivileges.html
[hba]: https://www.postgresql.org/docs/18/auth-pg-hba-conf.html
[locks]: https://www.postgresql.org/docs/18/explicit-locking.html#ADVISORY-LOCKS
[pg-restore]: https://www.postgresql.org/docs/18/app-pgrestore.html
[archive]:
  https://github.com/postgres/postgres/blob/REL_18_STABLE/src/bin/pg_dump/pg_backup_archiver.c#L3018-L3029
[promote]:
  https://docs.oracle.com/en-us/iaas/Content/secret-management/Tasks/promote-secret-version.htm
[oci]:
  https://docs.oracle.com/en-us/iaas/Content/Identity/Reference/keypolicyreference.htm
[podman]: https://docs.podman.io/en/latest/markdown/podman-secret-create.1.html
[libpq]: https://www.postgresql.org/docs/18/libpq-connect.html
[pgx]: https://github.com/jackc/pgx/blob/v5.12.0/pgconn/config.go
[peer]: https://www.postgresql.org/docs/18/auth-peer.html
[comment]: https://www.postgresql.org/docs/18/sql-comment.html
[descriptions]: https://www.postgresql.org/docs/18/catalog-pg-shdescription.html
[role-settings]:
  https://www.postgresql.org/docs/18/catalog-pg-db-role-setting.html
[large-objects]: https://www.postgresql.org/docs/18/lo-funcs.html
