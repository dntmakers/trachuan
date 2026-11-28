# Framework entitlement admin portal

Proposed under [ADR 0016](../adr/0016-control-frameworks-corpus.md), decisions
4, 9 and 10. The owner can list live entitlements, allow and revoke a scope, and
read the audit history. The [framework access note](frameworks-access.md) owns
entitlement meaning, the restore rule and the places for licensed material.

## Choices for owner approval

- Approve `admin.trachuan.vn` as the sole Cloudflare proxy exception in
  [ADR 0006](../adr/0006-single-host-oci-podman.md), including Universal SSL and
  the resulting zone-wide CAA additions. Approve Cloudflare processing the
  operator identity and entitlement and audit metadata on that hostname.
- Approve one explicit owner identity through a single IdP with independent
  Access MFA at each login and a 15-minute application session. Use the same
  canonical email as the sole grant recipient while the licence review is
  pending. Supply the identity and record the Access configuration privately.
- Approve the server-rendered interface and an estimated 128 MiB of host memory
  for one connector. Verify plan features and actual memory headroom before
  enabling the portal. A paid plan or larger host needs separate approval.

## Hostname and listener

`https://admin.trachuan.vn` goes through Access and a dedicated remotely managed
Cloudflare Tunnel to HTTP on `/run/trachuan-admin/admin.sock`. The Go server
creates a second listener and serves it with its own mux. Mount the socket
directory only in the server and connector containers, writable by the server
and read-only in the connector. Limit socket access to their mapped identities
and keep SELinux enforcing. There is no published TCP port. Caddy, Nuxt, the
public Go mux and the MCP handler have no forwarding or route for the portal.
The admin mux checks the exact `Host` before authentication and ignores
forwarded host and scheme headers.
[Tunnel service types](https://developers.cloudflare.com/tunnel/concepts/routing/#supported-protocols)

The portal defaults off. When enabled, require `SERVER_ADMIN_HOST`,
`SERVER_ADMIN_SOCKET`, `SERVER_ADMIN_ACCESS_TEAM_DOMAIN`,
`SERVER_ADMIN_ACCESS_AUD` and the Vault-fed `server_admin_owner_email`. Invalid
configuration or a socket bind failure keeps the portal unavailable and raises
an operator alert. Readiness for the admin listener includes the auth schema and
a usable key set. Public serving and the Caddy readiness gate are independent of
Cloudflare and its key endpoint.

## Access JWT verification

Cloudflare Access issues an application JWT. The browser uses the
`CF_Authorization` cookie at the edge. At the origin, require exactly one
`Cf-Access-Jwt-Assertion` header with one compact JWT. Cookies, `Authorization`,
email headers and forwarded identity headers confer no admin authority.
[Cloudflare JWT validation](https://developers.cloudflare.com/cloudflare-one/access-controls/applications/http-apps/authorization-cookie/validating-json/)

- Use the existing Go JOSE library to allow only `RS256`. Verify the signature
  against the RSA signing key with the matching `kid`. Require `iss` equal to
  the configured `https://<team>.cloudflareaccess.com` and require the
  application's configured AUD tag in `aud`. Require `type=app`, a nonempty
  `sub` and a signed `email` that parses to the configured canonical owner
  email. Reject service credentials and global session tokens. Require `iat` and
  `exp`, enforce `nbf` when present, reject expired or future assertions with at
  most 30 seconds of skew, and cap `exp - iat` at the application session
  duration.
  [Cloudflare application token](https://developers.cloudflare.com/cloudflare-one/access-controls/applications/http-apps/authorization-cookie/application-token/)
- Fetch `https://<team>.cloudflareaccess.com/cdn-cgi/access/certs` from the
  exact configured team over HTTPS with normal certificate checks. Accept only
  the canonical team origin, with no custom port, path, user info, query or
  fragment. Allow no redirects, token-supplied key locations or request-derived
  fetch URLs. Use only the `keys` array and reject ambiguous key IDs. Load at
  startup and refresh every five minutes, with one coalesced and rate-limited
  refresh on an unknown `kid`. Bound each fetch to 5 seconds and 64 KiB. Replace
  the whole key set after each successful refresh. A failed fetch can use the
  last known matching key for at most 15 minutes since the last success. After
  that, or when the key is unknown, deny admission and surface verifier failures
  in metrics. Access normally rotates keys every six weeks and publishes the old
  key for seven days.
  [Cloudflare signing keys](https://developers.cloudflare.com/cloudflare-one/access-controls/applications/http-apps/authorization-cookie/validating-json/#access-signing-keys)

Check the JWT on every request before route dispatch, including POST, redirects
and errors. Missing or invalid credentials yield a fixed 403. A fetch failure
that leaves no usable key for the assertion yields a fixed 503. Never echo the
JWT or its cookies. Persist the actor as `access:<team-host>/<sub>` using
verified claims and configuration. Cloudflare's subject belongs to an email in
one Zero Trust organization and can change if the operator is removed and added
again. Preserve the old actor identifier in audit history.
[Cloudflare identity claims](https://developers.cloudflare.com/cloudflare-one/access-controls/applications/http-apps/authorization-cookie/application-token/#identity-based-authentication)

## Routes and UI

Render two pages in Go using escaped HTML templates and plain forms. This keeps
browser, routing and JWT checks in one process. A separate Nuxt page set would
reuse UI components and require its own origin routing and credential handoff.
The portal has no product login, browser scripts, third-party assets, telemetry
or public navigation link.

| Route                       | Input                                                   | Success                                                                                                                                     |
| --------------------------- | ------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------- |
| `GET /`                     | None                                                    | `303` to `/entitlements`                                                                                                                    |
| `GET /entitlements`         | Optional positive `after` entitlement ID                | `200` HTML with canonical email, scope, grant time, actor, and forms. Live rows only.                                                       |
| `GET /entitlements/events`  | Optional positive `before` event ID                     | `200` HTML with event and entitlement IDs, occurrence time, actor, action, canonical email, scope and reason. Includes restore revocations. |
| `POST /entitlements/allow`  | `scope`, `form_token`, and `confirm_all=yes` for `*`    | `303` to `/entitlements` after commit                                                                                                       |
| `POST /entitlements/revoke` | Positive `id` of the rendered live row and `form_token` | `303` to `/entitlements` after commit                                                                                                       |

Use 50 rows per page, with a next link only when a further row exists. List live
rows in `(email, scope)` order, with `after` resolving to the anchor row through
its immutable ID, even after revocation. Events use `id DESC` with
`id < before`. Return 400 for malformed input or an unknown live-list anchor.
Display dates in UTC. The events page links back to the live list and to the
latest events. Pagination reads current state, and a refresh shows changes that
landed behind the displayed page. Only numeric row IDs appear in URLs.

The allow form shows the configured recipient as read-only text, with a
framework code or `*` as the scope. `*` needs a separate explicit checkbox
labelled for all licensed frameworks. Revoke acts on the displayed exact row.
Explain that any overlapping live scope can still give access after revoke.
Never expose notes, source files, framework content or account lookup results.

Only POST mutates. Require one `Origin` equal to `https://admin.trachuan.vn` and
`application/x-www-form-urlencoded`, with no CORS. Reject missing Origin,
unknown or repeated fields and body values from the URL. Each mutation form has
a random, single-use `form_token` bound to the verified assertion's digest, the
route, and the row ID for revoke. Keep it in a bounded in-memory set, valid for
at most five minutes and no later than JWT expiry. Allocate at most 512 live
form tokens and consume a token atomically before calling the store. An expired,
spent or mismatched token returns 403 with a link to reload. When a timeout
leaves the result uncertain, the operator reloads the list and events to check
before submitting a fresh form. Avoid automatic mutation retries.

Unknown routes return 404 and unsupported methods return 405. Request and
database deadlines reuse the [server budgets](budgets.md#request-limits). Return
413 for an oversized body, 415 for the wrong media type, 409 for a duplicate
allow or stale revoke, and 503 for an unavailable store or full admission
budget. Limit outstanding admin requests independently of public traffic.
Disable storage and cross-origin access for all portal responses. Send
`Cache-Control: no-store`, `Referrer-Policy: same-origin`,
`X-Content-Type-Options: nosniff` and a CSP with `default-src 'none'`,
`frame-ancestors 'none'`, `base-uri 'none'` and `form-action 'self'`.
`same-origin` preserves the browser's `Origin` on same-origin form POSTs and
withholds the referrer across origins. Logs carry route name, outcome and
latency. Exclude URL queries, headers, forms, row data and credentials from logs
and metrics.
[Fetch Origin algorithm](https://fetch.spec.whatwg.org/#append-a-request-origin-header)

## Store and audit

- Use `AllowFrameworkEntitlement` with the configured owner email, the shared
  email and scope parsers, verified actor and an empty note. The scope grammar
  matches the CLI, including the ability to allow a code before its release.
  `InsertFrameworkAllowance` writes the row and the `allow` event in one
  statement. The live uniqueness constraint arbitrates concurrent allows.
- Extend `RevokeFrameworkEntitlement` and `ApplyFrameworkRevocation` with an
  optional expected row ID. The portal passes the displayed ID and takes the
  canonical email and scope from the server's stored form state. The update and
  `revoke` event use the same statement with `revoked_at IS NULL`, so a stale
  form cannot revoke a replacement entitlement. The CLI continues to target the
  canonical email and scope through the same statement. When no live row
  matches, return a conflict and write no event. Preserve reason `operator`.
- Extend the store's `ListFrameworkEntitlements` selector with a bounded live
  page. Keep the CLI's current filtering, ordering and output. Add a bounded
  reader for `framework_entitlement_event`; the table's primary key supports the
  history page. Use parameterized queries and select only fields the portal
  renders. The portal shares the auth database and the existing events. No data
  backfill, extra writer, cache of entitlement decisions or framework release
  schema change is needed.

## Cloudflare and OCI

The owner creates a self-hosted Access application for the entire hostname.
Allow one exact email with the approved IdP, MFA and session settings. Check for
overlapping policies. Exclude path exceptions, Bypass and Service Auth. Record
the team origin and AUD and apply an admin-host cache bypass.
[Self-hosted application](https://developers.cloudflare.com/cloudflare-one/access-controls/applications/http-apps/self-hosted-public-app/)
[Access MFA](https://developers.cloudflare.com/cloudflare-one/access-controls/policies/mfa-requirements/#configure-independent-mfa-for-an-application)
[Session durations](https://developers.cloudflare.com/cloudflare-one/access-controls/access-settings/session-management/#session-durations)

Access allows enrollment of the first authenticator without an existing factor.
Before initial enrollment or any MFA reset, set the operator hold and close the
origin route. Verify the enrolled authenticator list after enrollment, then
revoke all existing sessions for the Access application. With the origin route
closed, the owner must sign in afresh and complete an independent Access
challenge before reopening the route and clearing the hold.
[First enrollment](https://developers.cloudflare.com/cloudflare-one/access-controls/access-settings/independent-mfa/#mfa-verification-for-authenticator-changes)
[Revoke application sessions](https://developers.cloudflare.com/cloudflare-one/access-controls/access-settings/session-management/#per-application)

Create a dedicated tunnel and one hostname route to the Unix socket, with a 404
catch-all. Enable **Protect with Access** for exactly that team and AUD and
retain Go verification. Publish the proxied admin CNAME to
`<tunnel-id>.cfargotunnel.com` under the existing Cloudflare DNS ownership rule.
The route workflow may create its DNS record automatically. Keep the public apex
and `www` DNS-only. Enable Universal SSL and verify issuance, effective CAA
records and continued Let's Encrypt renewal eligibility for Caddy before opening
the route. Cloudflare adds CAA permissions when Universal SSL and existing CAA
records coexist.
[Tunnel routing](https://developers.cloudflare.com/tunnel/concepts/routing/#published-applications)
[Protect with Access](https://developers.cloudflare.com/tunnel/reference/origin-parameters/#access)
[Universal SSL](https://developers.cloudflare.com/ssl/edge-certificates/universal-ssl/enable-universal-ssl/)
[CAA](https://developers.cloudflare.com/ssl/edge-certificates/caa-records/#caa-records-added-by-cloudflare)

For OpenTofu, review `deploy/oci/network.tf`, `vault.tf` and `iam.tf`. Add no
inbound rule, public IP, compute instance or Cloudflare provider. Ensure the OCI
and host egress policies permit the connector to reach documented Cloudflare
endpoints on port 7844 and both verifiers to reach the team key endpoint on
HTTPS. Scope the updater's Vault reads to the required secrets. The owner
applies the reviewed OCI plan and puts the connector token and the owner email
in Vault outside OpenTofu values and state. Cloudflare stays under the owner's
Cloudflare configuration.
[Tunnel firewall](https://developers.cloudflare.com/tunnel/configuration/#firewall-rules)

Add a dedicated rootless `cloudflared` Quadlet on the same OCI host. Pin its
arm64 image digest as an upstream image in the signed release manifest. Share
only the socket directory. Give the connector an egress-only network with no
access to the application network, OCI metadata service, host control socket or
database secrets. Use a file-mounted Podman secret for its own token and
`--token-file`; the issuer origin and AUD are configuration. Reconcile and
rotate the token through the [Vault workflow](deployment.md#secrets) and
recreate only the connector on token change. Confirm the chosen
[Cloudflare plan](https://www.cloudflare.com/plans/zero-trust-services/) covers
these settings. Measure connector memory at startup, idle and reconnect, and
check headroom under the existing serving load. Record final limits in
[budgets](budgets.md) and [host budgets](budgets-host.md).
[Connector token file](https://developers.cloudflare.com/tunnel/reference/run-parameters/#token-file)

## Threats and release tests

| Attacker                                 | Path and boundary to prove                                                                                                                                                                                                                                                                                                                                                                                                                                            |
| ---------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Anonymous visitor                        | Access denies at the edge. Direct origin and public mux requests, even with the admin `Host` or a valid test assertion, reach no admin handler. The socket rejects missing or forged assertions before a store call.                                                                                                                                                                                                                                                  |
| Signed-in user targeting another account | A product session, another Access email and all cross-origin writes fail. The browser cannot select a grant recipient or audit actor. A row ID is usable for revoke only in its bound form.                                                                                                                                                                                                                                                                           |
| Hostile MCP client or CIMD document      | Public MCP credentials and client metadata cannot reach the admin mux or become a key fetch URL. Check the existing CIMD private-address rejection and no admin paths in discovery.                                                                                                                                                                                                                                                                                   |
| Stolen token or cookie                   | Product credentials have no admin authority. A stolen valid Access application credential can act until edge revocation or expiry, including reading forms. Close the tunnel route, revoke Access sessions and check the audit events for unwanted mutations. Local JWT verification alone cannot see edge revocation. [Access revocation](https://developers.cloudflare.com/cloudflare-one/access-controls/access-settings/session-management/#revoke-user-sessions) |
| Network position between client and edge | HTTPS protects the browser and Tunnel encrypts the edge-to-connector leg. Prove the certificate and that HTTP serves no portal content. The final hop stays on the local socket. Cloudflare terminates browser TLS and is trusted with the admin metadata. [Tunnel encryption](https://developers.cloudflare.com/cloudflare-one/networks/connectors/cloudflare-tunnel/configure-tunnels/#secure-your-tunnel)                                                          |
| Compromised host container               | Web or Caddy has no socket mount, connector token or auth DB credentials. A compromised connector can replay an assertion through the socket. A leaked connector token also lets an off-host replica receive operator traffic. Use the tunnel compromise procedure below. A compromised server can bypass the portal and tamper with audit. [Audit limits](frameworks-access.md#classes-and-entitlements)                                                             |
| Leaked backup                            | The backup can disclose recipient and audit metadata. It cannot mint an Access JWT or a connector token. Keep portal ingress held through the auth restore fence and verify every restored live grant is revoked, with its event, before traffic resumes. [Backup boundary](deployment.md#backups)                                                                                                                                                                    |

Unit tests use local signed JWTs and a fake JWKS endpoint. Cover middleware
order, wrong issuer and AUD with a valid signature, wrong key or algorithm,
invalid claims, duplicate headers, expiry and clock skew, refresh throttling,
rotation, stale keys and restart. Cover malformed forms and cursors, escaping,
cache headers, cross-origin POST, token replay and store error mapping.

PostgreSQL integration tests drive CLI and HTTP with synthetic data and assert
one audit event per committed mutation, rollback if the event write fails,
concurrent allow and revoke, stale revoke after a re-allow, pagination and
restore history. Verify revocation on web API and MCP. With production headers,
a real browser must send the same-origin `Origin` and receive `303` for both
forms. Use test keys and verify list and history. On the host, prove SELinux
socket isolation and that the connector cannot reach the auth database. Attempt
a deploy between PostgreSQL readiness and the restore fence commit. Repeat after
an interruption releases the lock. Both ingresses must stay closed. Test hold
persistence on restart and failed activation, Access login and anonymous denial,
and Go's independent JWT rejection.

For a suspected tunnel compromise, hold admission in Go, stop the local
connector and close the edge route. Revoke Access sessions, then rotate the
tunnel token and force-disconnect all existing tunnel connections at Cloudflare.
Rotation alone leaves existing connections active. Reconcile the new token and
restart the trusted connector with the edge route closed. Compare its connector
ID with the active replica list and reopen only when it is the sole replica.
Review the entitlement audit log for unwanted changes.
[Tunnel token rotation](https://developers.cloudflare.com/tunnel/reference/tunnel-tokens/#rotate-a-token)
[Tunnel replicas](https://developers.cloudflare.com/tunnel/configuration/#replicas-and-high-availability)

## Ingress holds

Before activation the updater sets the deploy hold, stops the connector and
drains admin requests. It clears its hold after readiness. Failure leaves admin
ingress held. The admin listener and connector honor the site's shared restore
ingress hold in [backups](backups.md#restore-ingress-hold), including after
reboot. Go checks all holds on each admin request. Connector startup checks the
deploy and restore holds. Unreadable state fails closed. Only the operator
clears the hold used during launch, MFA reset and incidents.

## Release order and rollback

1. Approve and review the design. The primary records the ADR 0006 exception and
   updates [deployment](deployment.md#domain-and-dns). Before launch, both
   languages in `apps/web/i18n/legal.ts` disclose Cloudflare admin sign-in and
   entitlement and audit traffic. The public apex reaches Caddy directly. Keep
   [entitled-only serving](frameworks-access.md#rollout) in force.
2. Configure Access, create the dedicated tunnel with the Go route closed and
   apply the reviewed OCI plan. Grant the updater Vault read access and create
   `cloudflared_tunnel_token` and `server_admin_owner_email` in Vault.
3. Activate a [signed](pull-deployment.md) compatible release with `upstream`
   containing only `caddy` and no new Quadlet `Secret=` declarations. Install
   the ingress holds and a verifier that accepts exactly `caddy` for bundles
   without the connector and `caddy` plus pinned `cloudflared` for bundles with
   it. Its updater knows both new secrets and the connector consumer. Confirm
   host activation and reconciliation of both Podman secrets before publishing
   the next release. Check only secret existence and reconciled versions.
4. Set [`release_floor`](release-floor.md) to the first compatible release's
   sequence in a signed follow-up. Confirm the host's `deploy_release_floor` is
   at least that sequence before deploying the connector. Rehearse signed
   rollback on isolated state with the connector release active and the
   compatible release retained. Verify rollback removes the connector, keeps
   admin ingress held and preserves rows and events. Include an older retained
   target and prove rollback below the floor is refused. Deploy the signed
   connector release with both Quadlet secrets and the portal off. Run
   `make pre-push`, `make test`, `make test-integration` and `make e2e` and
   require green CI on each exact release commit.
5. Enable the listener under the operator hold. Verify edge TLS, HTTPS redirect,
   socket isolation and MFA. Publish the route, clear the hold and run a
   read-only smoke. Prove writes on scratch data. Keep corpus releases separate.
   Set the hold and close the route to take the portal offline. Recover via CLI.
