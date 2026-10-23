# Platform refresh (October 2026)

## Goal

Bring the project back in line with what Claude Code, Codex, Gemini CLI and the build stack
do today, fix the defects that drift caused, and make the next drift show up by itself
instead of in a manual audit.

Drift audit, 2026-10-23 (pinned 2026-09-22):

| Area | Finding |
|---|---|
| `deploy/Dockerfile` | `golang:1.22-alpine` cannot build a `go 1.24` module: `go: go.mod requires go >= 1.24 (running go 1.22.12; GOTOOLCHAIN=local)`. `alpine:3.20` is past end of life. |
| Codex | 19 new top-level `requirements.toml` keys, one removed (`allow_locked_computer_use`). OpenAI's Agent Security now assigns cloud-managed requirements per user, group and role, with priority order. |
| Claude Code | 13 nested blocks in the vendored schema reject unknown keys, and schemastore lags the CLI, so new valid keys such as `sandbox.allowNestedUserNamespaces` (CLI 2.1.305) fail at apply time. `policyHelper` contract unchanged. Server-managed settings still org-uniform. |
| Gemini CLI | v0.65.0: top-level keys and `PolicyDecision` unchanged. |
| Stack | Go 1.27.3 current. Node 20 end of life, Node 24 active LTS; CI runs Node 20. |

## Non-goals

- Pushing aw policies into OpenAI's policy API (Backlog).
- Delivering Claude policy as an organization mod (Backlog).
- Raising the `go.mod` floor above 1.24, or bumping frontend majors.
- Opening GitHub issues from the drift job: a failed scheduled run already notifies the owner.

## Decisions

1. **Codex positioning: reposition, keep support.** Per-group policy for Codex on ChatGPT plans is
   native now. The pitch becomes: per-repository rules, one policy across three agents, Codex on
   API keys, self-hosting. The per-group gap is claimed for Claude Code on claude.ai seats only.
2. **Claude schema lag: a dated overlay.** Two shapes considered:
   (a) inject extra properties into the parsed schema before compiling;
   (b) validate, then drop "additional property" causes that match an allowlist.
   Choose (a): it leaves the compiler's messages untouched and does not parse error text.
   Each overlay entry carries a value schema (`{"type":"boolean"}` and similar), not "anything",
   because an unrepairable value in helper output makes Claude Code refuse to start.
3. **Drift detection: a weekly CI job** running `go test -tags drift ./internal/drift/`, which
   fetches upstream references and compares them with the pinned lists. Unit tests stay offline.
4. **Build image:** `golang:1.27-alpine` and `alpine:3.22`; `go.mod` keeps `go 1.24`.
   CI builds the image so this cannot rot again.
5. **CI Node:** 24 in both workflows.

## Design

- `internal/agent/codex/requirements/keys.go`: refreshed list, `KeysAsOf = "2026-10-23"`.
- `internal/agent/claude/schema/overlay.go`: `KeysAsOf`, and `Overlay []Extra` where
  `Extra{Path string /* dotted settings path, e.g. "sandbox.allowNestedUserNamespaces" */; Schema map[string]any}`.
  `load()` walks `properties` (following local `$ref`) from the root to the parent block and
  adds the property before compiling. An entry whose parent cannot be resolved, or that the vendored
  schema already defines, is a compile error, so a stale overlay fails the test suite rather than a fleet.
  Seeded with every nested key in the settings reference that the vendored schema lacks, as of 2026-10-23.
- `internal/drift/drift_test.go` (`//go:build drift`): one test per source:
  Codex `requirements.toml` keys (learn.chatgpt.com config reference, `.md`),
  Gemini top-level keys and `PolicyDecision` (gemini-cli `main`),
  Claude nested keys (settings reference `.md` index) against vendored schema ∪ overlay.
  Each failure names the added and removed keys and the file to edit. A parse failure is a failure
  that says "the upstream page changed shape".
- `.github/workflows/drift.yml`: `schedule` weekly plus `workflow_dispatch`.
- Copy: README "What remains uncovered" and `web/content/home.ts` "Rules per group on seat plans"
  state the gap for Claude Code seats and name OpenAI Agent Security for Codex.

## Acceptance criteria

1. The image builds: `docker build -f deploy/Dockerfile .` exits 0, and CI runs the same build.
2. A Codex policy using each of the 19 new keys validates, and `allow_locked_computer_use` is rejected
   with a message naming 2026-10-23: `go test ./internal/agent/codex/requirements/ -run TestKeys -v`.
3. A Claude policy with `sandbox.allowNestedUserNamespaces: true` validates; the same key set to
   `"yes"` fails; `permissions.denny` still fails: `go test ./internal/agent/claude/schema/ -v`.
4. An overlay entry for a key the vendored schema already has, or under a parent that does not
   resolve, fails `go test ./internal/agent/claude/schema/`.
5. `go test -tags drift ./internal/drift/` passes against upstream on 2026-10-23, and fails with the key
   name when a pinned list has one key removed (checked by a fixture test in the same package that
   runs offline: `go test -tags drift ./internal/drift/ -run TestDiffNames`).
6. `go test ./...` without the tag makes no network request (`internal/drift` has no non-tagged files).
7. Both workflows use Node 24, and `drift.yml` has `schedule` and `workflow_dispatch`:
   `grep -n "node-version: 24" .github/workflows/{console,web}.yml` and `grep -n "schedule\|workflow_dispatch" .github/workflows/drift.yml`.
8. No copy claims vendors cannot target groups without naming Claude Code:
   `grep -rn -i "group" README.md web/content` reviewed; web unit tests pass (`pnpm -C web test`).
9. Full suite green: `go vet ./... && go test ./...`.

## Security notes

Validator strictness decides what reaches a fail-closed `policyHelper`. The overlay widens the
accepted set only by named keys with typed value schemas; typos in strict blocks still fail.
The drift job is read-only, needs no secrets, and runs with `permissions: contents: read`.
