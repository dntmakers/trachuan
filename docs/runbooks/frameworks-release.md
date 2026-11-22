# Release a frameworks corpus

How a control frameworks release goes from the MacBook to the
[production host](host.md). The design is
[frameworks release](../design/frameworks-release.md); this runbook gives the
commands. Licensed material stays in the places of
[access](../design/frameworks-access.md#licensed-material-and-where-it-may-be):
nothing below may print, paste or upload framework text, and the measured index
size and server memory of a real release are private evidence.

## Before the first release

The order is [frameworks release](../design/frameworks-release.md#order). Check
each step before the next:

1. **Owner.** `server_frameworks_key` exists in Vault, generated as
   [Key and flag](../design/frameworks-release.md#key-and-flag) says, and
   OpenTofu is applied (`deploy/oci`: the `trachuan-frameworks-releases` bucket,
   the `trachuan-frameworks-verify` user and group, the upload, verify and host
   statements). The verify user's customer secret key is stored in the GitHub
   environment `frameworks-release` as `FRAMEWORKS_READ_KEY_ID` and
   `FRAMEWORKS_READ_KEY_SECRET`, with the variable `OCI_S3_ENDPOINT`.
2. **Image A** is live: its policy lists `server_frameworks_key` and its updater
   has reconciled it. On the host, as `trachuan`:

   ```sh
   sudo -iu trachuan
   export XDG_RUNTIME_DIR=/run/user/$(id -u)
   podman secret ls --format '{{.Name}}' | grep -x server_frameworks_key
   ```

   No output means the secret is missing: image B would fail every run at step
   `secrets` with `secret missing: server_frameworks_key` and leave the serving
   image in place. Do not tag image B until this prints the name.

3. **Image B** is live: `deploy/host/deploy-policy.json` has the `frameworks`
   block, the server Quadlet mounts the key and `server.env` sets
   `SERVER_FRAMEWORKS=false`. The phase waits with reason `release floor` until
   the stored release floor reaches the block's `min_release_floor`. Before a
   release is published, the result reads `current` with "no frameworks release
   published":

   ```sh
   sudo cat /srv/trachuan/state/deploy/release-frameworks.json
   sudo tail -n 20 /var/log/trachuan/release-frameworks.log
   ```

## Build on the MacBook

`corpus frameworks build <rel>` runs every step in order, offline; the contract
is [frameworks build](../design/frameworks-build.md). It reads the licensed
files from `FRAMEWORKS_SOURCE_DIR` (the owner's clone of `compliary-private`)
and writes under `FRAMEWORKS_WORK_DIR/<rel>/`. Both must be absolute and outside
every synced folder; the command refuses iCloud Drive, Dropbox, Google Drive,
OneDrive, `~/Desktop` and `~/Documents`.

```sh
set -a; . ./.env; set +a
export FRAMEWORKS_SOURCE_DIR=/absolute/path/to/compliary-private
export FRAMEWORKS_WORK_DIR=/absolute/path/outside/sync/frameworks-work
export CORPUS_BUILDER_DATABASE_URL="postgres://corpus_builder:${BUILDER_PASSWORD}@127.0.0.1:25433/postgres"
export CORPUS_PG_DUMP=/opt/homebrew/opt/postgresql@18/bin/pg_dump
export CORPUS_PG_RESTORE=/opt/homebrew/opt/postgresql@18/bin/pg_restore
REL=20261123
corpus frameworks build "$REL"
corpus frameworks release-row "$REL"
```

The `embed` step calls the document embedder at `CORPUS_EMBEDDER_URL` (default
`http://127.0.0.1:8092`) and `evaluate` the Go query embedder at
`CORPUS_QUERY_EMBEDDER_URL` (default `http://127.0.0.1:8091`,
[local corpus](corpus-local.md)); start both first.

A rerun skips every step directory that still validates and stops at the first
that does not, naming it; remove that directory and every later one, or start a
new `rel`. A failed step leaves only a `.tmp-*` directory, removed by the next
run. There is no gap acceptance: a blocking gap (`ocr_blocked`, `title_missing`
on a `licensed` version) makes `quality_pass` false, and the uploader refuses
the release.

## Upload and sign

The uploader takes `FRAMEWORKS_WORK_DIR/<rel>/` and needs the AWS CLI profile,
`OCI_S3_ENDPOINT`, `flock`, `oras`, signed-in `gh` and cosign v3.1.4, as for law
([release a corpus](release-restore.md#release-a-corpus)). It shares the law
uploader's lock and never overwrites a prefix.

```sh
REPOSITORY=dntmakers/trachuan
bash scripts/corpus-upload.sh --dry-run --repository "$REPOSITORY" frameworks "$REL"
bash scripts/corpus-upload.sh --repository "$REPOSITORY" frameworks "$REL"
```

Dry-run validates the files and prints the upload and dispatch plan; its last
line is the dispatch command. The real run uploads the five objects to
`frameworks/<rel>/` in `trachuan-frameworks-releases`, rechecks every size, and
dispatches `frameworks-release.yml` from `main` with the SHA-256 of the report
it validated. The first release names no previous report; every later one must
name the active `frameworks-production` release's report. If only the dispatch
failed, do not upload again: run the dry-run with the same arguments and run its
last line.

The workflow signs the manifest and pushes it to
`ghcr.io/dntmakers/trachuan-corpus-release` as `frameworks-<rel>` and moves
`frameworks-production`. Its job holds nothing after it ends.

## Activation on the host

The updater's `frameworks` phase runs after the law phase, under the same locks,
and activates the release as the law phase does: restore into
`frameworks_<rel>`, checks, smoke, pointer swap
([host activation](../design/frameworks-release.md#host)). The first release is
accepted only with no `frameworks_release` row (bootstrap); after that, a new
release must name the active one as `previous`. Watch the same two files as in
step 3 and the `release_failed` and `release_waiting_age_seconds` metrics with
`kind` `frameworks` ([alarms](alarms.md)). A law activation never writes the
frameworks pointer, and the reverse.

`waiting` with a schema, recipe or query hash reason means the serving image
does not support this release; a newer image or a release built for this one
clears it.

## Measure memory with both corpora active

The flag stays off until this measurement fits
[host budgets](../design/budgets-host.md#host-memory-estimates). It needs the
host, so the coordinator runs it. Check that at least 3 GB is free first
(`free -m`): the probe runs beside the serving server.

On the host, as `trachuan`, start a one-off server from the serving image on a
loopback port that Caddy does not route, with the flag on:

```sh
sudo -iu trachuan
export XDG_RUNTIME_DIR=/run/user/$(id -u)
SERVER_IMAGE=$(grep '^SERVER_IMAGE=' /srv/trachuan/host/images.env | cut -d= -f2-)
podman run -d --rm --name trachuan-server-memcheck --network host \
  --user 65532:65532 --cap-drop=ALL --security-opt=no-new-privileges --read-only \
  --memory=2500m --env-file /srv/trachuan/host/server.env \
  -e SERVER_LISTEN=127.0.0.1:18080 -e SERVER_FRAMEWORKS=true \
  --secret google_client_secret,type=env,target=GOOGLE_CLIENT_SECRET \
  --secret server_auth_database_url,type=env,target=SERVER_AUTH_DATABASE_URL \
  --secret server_corpus_database_url,type=env,target=SERVER_CORPUS_DATABASE_URL \
  --secret account_keys,type=mount,target=/run/secrets/account_keys,uid=65532,gid=65532,mode=0400 \
  --secret server_frameworks_key,type=mount,target=/run/secrets/server_frameworks_key,uid=65532,gid=65532,mode=0400 \
  --secret smtp_username,type=env,target=SERVER_SMTP_USERNAME \
  --secret smtp_password,type=env,target=SERVER_SMTP_PASSWORD \
  "$SERVER_IMAGE" serve
```

Warm both kinds with one search each, using a personal token typed at a hidden
prompt and passed to `curl` on standard input, never on the command line:

```sh
read -rs TOKEN
mcp() {
  printf 'header = "Authorization: Bearer %s"\n' "$TOKEN" |
    curl -sS -K - -o /dev/null -w '%{http_code}\n' \
      -H 'Content-Type: application/json' -H 'Accept: application/json, text/event-stream' \
      --data "$1" http://127.0.0.1:18080/mcp
}
mcp '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"search","arguments":{"country":"vn","query":"lãi suất cho vay"}}}'
mcp '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"search","arguments":{"corpus":"frameworks","query":"access control"}}}'
unset TOKEN
```

Both print `200`. Then record the probe's resident memory and stop it:

```sh
PID=$(podman inspect --format '{{.State.Pid}}' trachuan-server-memcheck)
grep -E '^(VmRSS|VmHWM)' "/proc/$PID/status"
podman stop trachuan-server-memcheck
```

Compare `VmHWM` with the server row and its 2,500 MB container limit. Write the
figures only to private evidence under `FRAMEWORKS_WORK_DIR` and give them to
the coordinator; they are derived from licensed files and never enter Git, a
pull request or a log.

## Entitlement and flag

**Owner decision.** The owner's entitlement is the only one until the licence
terms review. On the host, as `trachuan`:

```sh
podman exec trachuan-server /app/server admin frameworks allow \
  --email <owner email> --scope '*' --by owner --note 'owner entitlement'
podman exec trachuan-server /app/server admin frameworks list
```

`revoke` takes the same `--email`, `--scope` and `--by`. Turning the flag on is
an image change (`SERVER_FRAMEWORKS=true` in `deploy/host/server.env`), made
only on the owner's decision after the memory check. Acceptance then runs with
an entitled and a non-entitled account: the non-entitled one sees reduced
records for licensed versions and full text for `free` ones.

## Retry and roll back

Dispatching `frameworks-release.yml` again for the same `rel`, with
`report.sha256` from its signed `frameworks-<rel>` manifest, signs a new
sequence and retries a failed activation. Dispatching it for an older retained
`rel` with that release's signed report hash is a rollback; the host reactivates
the retained database. A manual swap of `frameworks_release.active_db` leaves
`frameworks-production` behind, and the next upload refuses until the two agree.

```sh
gh workflow run frameworks-release.yml --repo dntmakers/trachuan --ref main \
  -f rel=<rel> -f report_sha256=<signed report SHA-256>
```
