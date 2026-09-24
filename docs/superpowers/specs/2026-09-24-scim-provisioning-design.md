# SCIM provisioning: the identity provider pushes memberships to the control plane

Date: 2026-09-24. Status: approved, not yet implemented. Extends
`2026-09-22-idp-group-sync-design.md`, which left "pulling from any IdP,
and SCIM" out of scope because either would produce the same memberships
the snapshot holds. This is the SCIM half. A puller stays out of scope.

## Why this exists

The snapshot endpoint makes the operator the integration: someone has to
write and run an exporter on a cron. Okta and Entra ID already know how to
provision an application over SCIM 2.0 (RFC 7643/7644): an admin adds the
app, pastes a base URL and a token, assigns groups, and the IdP pushes
every create, change and deactivation as it happens. One protocol covers
both vendors, needs no IdP credentials inside `awd`, and removes the cron.

## Decisions

- **SCIM server in `awd`**, not a puller. The IdP pushes; `awd` never calls
  out.
- **A third membership source, unioned.** A user's groups are
  `authored ∪ snapshot ∪ scim`. Each source only ever removes what it added,
  as the group-sync design already says for the first two. An organization
  uses whichever feeds it has.
- **Deprovisioning drops SCIM groups only.** `active: false` or `DELETE` on
  a user removes the groups SCIM gave them from the next bundle. It never
  revokes a machine: revocation stays an explicit admin act, so a bad
  attribute mapping in the IdP cannot revoke a fleet.
- **Okta and Entra ID are the v1 targets**, quirks included. Anything else
  that speaks the same subset works too; nothing else is tested.
- **Normalized tables**, not a document or an op log. Each SCIM operation
  is a small transactional write, per-resource reads and filters are
  indexed lookups, and the store conformance suite pins both stores to the
  same behaviour.

## Architecture

### `internal/scim` — the protocol, with no HTTP and no store

- Resource types and their JSON: `User` (`id`, `userName`, `externalId`,
  `active`, `meta`) and `Group` (`id`, `displayName`, `externalId`,
  `members[].value`, `meta`), with the core schema URNs. Other attributes
  an IdP sends (`name`, `emails`, `title`, …) are accepted and dropped; a
  GET returns what the server holds. Both target IdPs tolerate that.
- A filter parser for exactly the forms the targets send:
  `userName eq "…"`, `displayName eq "…"`, `externalId eq "…"`. Anything
  else is an `invalidFilter` error, not a silently empty list.
- A PATCH applier (RFC 7644 §3.5.2) that turns a `PatchOp` body into a
  typed change set, with the vendor quirks normalized here and nowhere
  else:
  - `op` compared case-insensitively (Entra sends `Replace`, `Add`).
  - Boolean values given as strings `"True"`/`"False"` (Entra).
  - A `replace` with no `path` whose value is an object of attributes, e.g.
    `{"op":"replace","value":{"active":false}}` (Entra, and Okta for
    users).
  - Member removal by path filter `members[value eq "<id>"]` (Okta) and by
    a value list `{"op":"remove","path":"members","value":[{"value":"<id>"}]}`
    (Entra).
  - `replace` on `members` as a full set.
  - Paths the server does not hold (`name.givenName`, `emails[…]`) are
    no-ops, so an IdP does not retry a change forever.
- SCIM error bodies (`urn:ietf:params:scim:api:messages:2.0:Error`, with
  `status` and `scimType`).

### Model (`internal/model`)

```go
// SCIMUser is a user as the identity provider provisioned it. UserName is
// what resolution matches against the enrolled user.
type SCIMUser struct {
	ID         string // server-generated UUID
	UserName   string
	ExternalID string
	Active     bool
	Created    time.Time
	Modified   time.Time
}

// SCIMGroup is a provisioned group. Members are SCIMUser IDs; the group's
// DisplayName is the group name policies target.
type SCIMGroup struct {
	ID          string
	DisplayName string
	ExternalID  string
	Members     []string
	Created     time.Time
	Modified    time.Time
}
```

### Store

`store.Store` gains, with memory and Postgres implementations and
`storetest` conformance cases for every one:

- `CreateSCIMUser`, `SCIMUser(id)`, `ReplaceSCIMUser`, `DeleteSCIMUser`,
  `ListSCIMUsers(filter, startIndex, count) (users, total, err)`.
- The same five for groups.
- `PatchSCIMGroup(ctx, id, change)` — display name and member add, remove
  and replace applied in one transaction, so two concurrent member PATCHes
  from the IdP cannot lose a write.
- `SCIMGroupsFor(ctx, userName) ([]string, error)` — the display names of
  the groups that contain that user, **only if the user is active**; an
  empty result for an unknown or inactive user.
- `SCIMCounts(ctx) (users, activeUsers, groups int, err)` for the summary.

Rules the conformance suite fixes:

- `userName` is unique **case-insensitively** (the core schema declares it
  `caseExact: false`); a clash is `ErrConflict`. `displayName` is unique
  case-insensitively too, because a policy targets a group by name and two
  groups differing only in case would be ambiguous.
- Deleting a user removes it from every group; deleting a group removes its
  memberships.
- A member ID that is not a user is `ErrBadInput`; the group is unchanged.
- Listing is ordered by `Created`, then `ID`, so paging is stable.

Postgres: migration `0005_scim.sql` adds `scim_users` (unique index on
`lower(user_name)`), `scim_groups` (unique index on `lower(display_name)`)
and `scim_members (group_id, user_id)` with cascading foreign keys to both.

### Resolution

`policy.UnionGroups` becomes variadic, `UnionGroups(lists ...[]string)`,
still sorted, de-duplicated and never nil. `getBundle` resolves:

```
groups = UnionGroups(ruleSet.GroupsFor(user), snapshot.Members[user], scimGroups)
```

With no SCIM data the result is byte-for-byte what the server sends today,
so deploying this changes no ETag.

The match against the enrolled user stays **exact**, as the group-sync
design decided: no case folding in resolution. SCIM's case-insensitive
uniqueness only stops the IdP from creating two users that differ by case;
it does not make `Alice@acme.com` match an enrollment for `alice@acme.com`.
The resolve endpoint below makes that mismatch visible instead of hiding it.

The ETag covers the compiled body, so a membership change reaches a
machine on its next `aw-sync` cycle. No client change, no bundle format
change.

## API

### `/scim/v2` — for the identity provider

Auth: `Authorization: Bearer $AWD_SCIM_TOKEN`, a token separate from
`AWD_ADMIN_TOKEN` because it lives in the IdP's configuration, not with the
operators. Unset: 503 on every SCIM route. Wrong: 401. Constant-time
compare, as `requireAdmin` does. Errors use the SCIM error body.

| Route | Behaviour |
| --- | --- |
| `GET /ServiceProviderConfig` | patch true, bulk false, filter true (maxResults 1000), changePassword false, sort false, etag false, auth scheme oauthbearertoken |
| `GET /ResourceTypes`, `GET /Schemas` | static documents for User and Group |
| `GET /Users` | `filter`, `startIndex` (1-based, default 1), `count` (default 100, max 1000); `ListResponse` with `totalResults` |
| `POST /Users` | 201 + resource + `Location`; 409 `uniqueness` on a duplicate `userName` (Okta then looks the user up by filter and links it, as it expects to) |
| `GET /Users/{id}` | 200, or 404 |
| `PUT /Users/{id}` | replaces `userName`, `externalId`, `active` |
| `PATCH /Users/{id}` | the applier's user changes; 200 + resource |
| `DELETE /Users/{id}` | 204; memberships removed |
| `GET /Groups` | as `/Users`; honours `excludedAttributes=members` |
| `POST /Groups` | 201; 400 `invalidValue` when a member is not a user; 409 on a duplicate `displayName` |
| `GET /Groups/{id}` | 200 (honours `excludedAttributes=members`), or 404 |
| `PUT /Groups/{id}` | replaces `displayName`, `externalId`, `members` |
| `PATCH /Groups/{id}` | one transaction; 204 when no `attributes` are requested (Entra expects either 200 or 204) |
| `DELETE /Groups/{id}` | 204 |

Request bodies are limited to 1 MiB; 413 beyond it. SCIM changes are
small, and large groups arrive as member PATCHes, not as one body.

Deactivation: `active: false` keeps the user row and its memberships;
`SCIMGroupsFor` stops returning them. Reactivation restores them without
the IdP re-pushing a single membership.

### Admin additions

- `GET /v1/groups` (existing): the summary gains
  `"scim": {"users": N, "activeUsers": N, "groups": N}`. It is present
  whenever SCIM is configured, even with no snapshot; the 404 for "no
  snapshot" becomes a 200 with `snapshot: null` when SCIM holds data.
- `GET /v1/groups/resolve?user=<enrolled user>` (new, admin): 

  ```json
  {"user": "alice@acme.com",
   "authored": ["contractors"], "snapshot": [], "scim": ["platform"],
   "effective": ["contractors", "platform"],
   "scimNearMatch": null}
  ```

  `scimNearMatch` names a SCIM `userName` that equals the user when
  case-folded but not exactly — the misconfigured attribute mapping this
  design refuses to paper over — and is `null` otherwise.

### CLI

- `awd groups` prints the SCIM counts under the snapshot summary.
- `awd groups resolve <user>` prints the per-source breakdown and, when
  there is one, the near-match warning.

## Failure modes

| Where | Failure | Behaviour |
| --- | --- | --- |
| scim | `AWD_SCIM_TOKEN` unset | 503 on every SCIM route; bundles resolve authored ∪ snapshot |
| scim | wrong bearer | 401 |
| scim | malformed JSON, missing schema, unknown PATCH op | 400 `invalidSyntax`; nothing written |
| scim | unsupported filter | 400 `invalidFilter` |
| scim | duplicate `userName` or `displayName` | 409 `uniqueness` |
| scim | unknown id | 404 |
| scim | member that is not a user | 400 `invalidValue`; group unchanged |
| scim | body over 1 MiB | 413 |
| scim | store write fails | 500; transaction rolled back |
| bundle | SCIM lookup fails | 500, as a failed snapshot read does today; the machine keeps its cached bundle |

## Testing

- `internal/scim`: filter parser table (each accepted form, quoting and
  escapes, each rejected form); PATCH applier table built from **Okta and
  Entra request bodies as their provisioning docs show them**, every quirk
  above included; resource JSON round-trip.
- `storetest` conformance: CRUD for both resources; case-insensitive
  uniqueness on both names; delete cascades; a member that is not a user;
  concurrent member PATCHes on one group lose nothing; `SCIMGroupsFor`
  excludes an inactive user and restores on reactivation; stable paging
  across inserts. Postgres through the Docker path — and the deferred
  migration 0003 conformance run happens in the same session.
- handler: 503 and 401; each route's success and error cases; an **Okta
  provisioning sequence** and an **Entra provisioning sequence** replayed
  end to end (create user → create group → add member → deactivate →
  reactivate → remove member → delete), asserting an enrolled machine's
  bundle groups and ETag after each step; `GET /v1/groups` with SCIM data
  and no snapshot; `resolve` breakdown and near-match.
- `policy`: variadic `UnionGroups` — zero lists, one, three, nils.
- `cmd/awd` e2e: provision a user and group over SCIM, fetch the bundle as
  that user's enrolled machine, see the group; `awd groups resolve`.

## Documentation

- `README.md`: a "SCIM provisioning" section — Okta app setup (base URL
  `https://<awd>/scim/v2`, HTTP header auth, enable Create/Update/Deactivate
  Users and Push Groups), Entra enterprise app setup (tenant URL, secret
  token, map `userName` to whichever of `userPrincipalName` or `mail` equals
  the enrolled email), the three-source union, and that deprovisioning drops
  groups but does not revoke machines.
- The group-sync design gets one line pointing here.

## Out of scope, deliberately

- Revoking machines on deprovision.
- SCIM bulk, sorting, ETags and resource versioning.
- Storing user attributes beyond the ones resolution needs.
- Group-name mapping (`idp:eng-platform` → `platform`); the IdP can rename
  what it pushes.
- Google Workspace, JumpCloud and other IdPs as tested targets.
- A per-change audit log; `Modified` timestamps are the trail for now.
- An IdP puller.
