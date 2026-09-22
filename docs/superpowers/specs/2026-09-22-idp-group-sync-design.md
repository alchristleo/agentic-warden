# IdP group sync: membership snapshots pushed to the control plane

Date: 2026-09-22. Status: approved design, not yet implemented. Extends
`2026-09-21-multi-agent-bundle-sync-design.md`, "Group resolution", which
said "an identity-provider sync later replaces the map without touching
clients or the bundle format". This is that sync. One refinement to that
sentence: the sync does not replace the authored map, it unions with it
(see "Resolution").

## Why this exists

Group targeting is how a policy says "the platform team may use Opus" or
"contractors run read-only". Today the memberships live in the policy YAML,
authored by hand and versioned with the rules. That is right for a pilot
and wrong for an organization: memberships change daily and belong to the
identity provider, not to a policy file that needs a review to change.

The client side already does the right thing. `awd` resolves a machine's
user to groups when it serves the bundle, the bundle's ETag covers the
resolved body, and `aw-sync` re-fetches on the next cycle when the ETag
changes. Nothing on a machine knows or cares where a membership came from.
So the whole change is server-side: a place to hold memberships the IdP
knows, a way to put them there, and one lookup that reads both sources.

## Approach

Memberships arrive as a **snapshot pushed to an admin endpoint**. The
operator exports users and groups from their IdP with whatever tool they
already trust (Okta's API, Google Directory, Entra Graph, a SCIM bridge)
and posts the result on a cron. `awd` stores the snapshot and resolves
against it.

Why push and not pull: a puller needs one client per IdP, credentials for
that IdP inside `awd`, and a scheduler; each is its own project and each
is vendor-specific. A snapshot endpoint is IdP-agnostic, testable with a
fixture, and is what a puller or a SCIM front end would feed anyway, so
neither is ruled out later.

## Data model and store

```go
// GroupSnapshot is one export of IdP memberships. It replaces the previous
// snapshot whole: a user absent from it has no synced groups, which is what
// a cron export means and what makes a snapshot auditable.
type GroupSnapshot struct {
	// Source names the exporter, for the operator reading a listing.
	Source string
	// AppliedBy is who posted it, from the client's environment as with
	// revisions.
	AppliedBy string
	// SyncedAt is when the server stored it.
	SyncedAt time.Time
	// Members maps a user, as the enrollment names them, to their groups.
	Members map[string][]string
}
```

`store.Store` gains:

- `PutGroupSnapshot(ctx, s GroupSnapshot) error` — appends; the newest is
  current, as with revisions. `ErrBadInput` for an empty `Members` key or
  an empty group name.
- `CurrentGroupSnapshot(ctx) (GroupSnapshot, error)` — newest, or
  `ErrNotFound` when none has ever been posted.

Both stores implement it: memory, and Postgres with one migration adding
`group_snapshots (id bigserial, source text, applied_by text, synced_at
timestamptz, members jsonb)`. The `storetest` conformance suite gains the
cases below, so the two stores cannot drift.

## Resolution

`getBundle` resolves the machine's user as:

```
groups = policy.UnionGroups(ruleSet.GroupsFor(user), snapshot.Members[user])
```

`UnionGroups` returns the sorted, de-duplicated union and never a nil
slice. With no snapshot stored, the result is the authored map alone —
byte for byte what the server sends today.

Why union and not replace: the authored map becomes the manual override —
a contractor who is not in the IdP, a group the IdP does not model, an
emergency change while the export is broken — and nothing a policy author
wrote disappears the day the first snapshot lands. A membership that must
go away is removed from the source that added it.

User keys compare exactly against the enrolled user, as the parent design
says for groups. No case folding: an export whose keys differ from the
enrollment emails in case is an export to fix, and folding would hide that
until a user got the wrong policy.

The ETag covers the compiled body, so every machine whose groups changed
sees a fresh bundle on its next cycle. No client change, no bundle format
change.

## API

Both routes are admin routes: `Authorization: Bearer $AWD_ADMIN_TOKEN`,
401 on a wrong token, 503 when the server has no admin token configured —
exactly as `enroll-token`, `machines` and `revoke` behave.

`PUT /v1/groups` — body:

```json
{"source": "okta-export", "members": {"alice@acme.com": ["platform", "oncall"]}}
```

- 422 with a message naming the problem when `members` is missing or not
  an object, a user key is empty, a value is not a list of strings, or a
  group name is empty. An empty `members` object is valid: it means "the
  IdP says nobody is in anything", and the authored map still applies.
- Body limited to 8 MiB; 413 beyond it.
- 200 with `{"source": ..., "syncedAt": ..., "users": N, "groups": M}`
  where `groups` counts distinct group names.

`GET /v1/groups` — 200 with the same summary plus `"members"`, or 404
when no snapshot has been posted. It exists so an operator can check what
the server holds without reading the database.

## CLI

`awd groups apply <file>` reads JSON or YAML in the body's shape (the
same YAML→JSON loader `awd apply` uses), posts it, prints the summary.
`AppliedBy` comes from `AWD_APPLIED_BY`, `USER` or `USERNAME`, as for
revisions.

`awd groups` prints the current snapshot's summary: source, synced age,
users, groups; or "no group snapshot" and exit 0 when there is none.

Both use the existing admin request helper and `--url`/`AWD_ADMIN_TOKEN`
conventions.

## Failure modes

| Where | Failure | Behaviour |
| --- | --- | --- |
| awd | `AWD_ADMIN_TOKEN` unset | 503 on both routes (unchanged rule) |
| awd | wrong bearer | 401 |
| awd | malformed snapshot | 422 naming the key; previous snapshot untouched |
| awd | body over 8 MiB | 413; previous snapshot untouched |
| awd | store write fails | 500; previous snapshot untouched |
| awd | no snapshot yet | bundles resolve from the authored map; `GET /v1/groups` 404 |
| awd groups apply | unreadable or invalid file | exit non-zero, nothing posted |

A snapshot never affects a machine until its next `aw-sync` cycle, and a
machine that cannot reach the server keeps the bundle it has — the parent
design's outage behaviour, unchanged.

## Testing

- `policy`: `UnionGroups` — disjoint, overlapping, one side nil, both nil
  → `[]string{}`; sorted output.
- store conformance (`storetest`): put then current returns it with
  `SyncedAt` set; a second put replaces it; `ErrNotFound` before any put;
  `ErrBadInput` for an empty user key and an empty group name; Postgres
  migration applies (the existing Docker-backed test path).
- handler: 503 with no admin token, 401 with a wrong one, 422 for each
  malformed shape, 413 over the cap, 200 summary counts; `GET` 404 then
  200; the bundle for an enrolled user reflects the union of authored and
  synced groups, and its ETag differs from the pre-snapshot ETag; with an
  empty `members` object the bundle equals the authored-only bundle.
- `cmd/awd` e2e: `groups apply` a YAML fixture then `GET /v1/bundle` as an
  enrolled machine shows the union; `groups` prints the summary; without
  the admin token both commands refuse.

## Documentation

- `README.md`: a "Group membership" paragraph under the control-plane
  section — authored map is the manual override, the snapshot is the
  IdP's, they union, keys match the enrolled email exactly, `awd groups
  apply` on a cron; plus `examples/groups.yaml`.
- The parent design's "Group resolution" section gets one line pointing
  here.

## Out of scope, deliberately

- Pulling from any IdP, and SCIM. Both would produce exactly this
  snapshot; build them when an organization needs one.
- Group-name mapping or filtering (`idp:eng-platform` → `platform`). The
  exporter can rename; the policy can name IdP groups as they are.
- Per-user audit of membership changes. The snapshot history in the store
  is the audit trail for now.
- Case-folding or aliasing of user identifiers.
