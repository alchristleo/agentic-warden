# Plan: agy adapter (2026-10-26)

Spec: `docs/superpowers/specs/2026-10-26-agy-adapter-design.md`. Branch `agy-adapter`. Trust boundary: yes.
Audience-facing: yes (platform admins reading docs/site). Agent package: `internal/agent/agy`.
Copy the shape of `internal/agent/gemini` (renderer, managed/, Inspect) and its tests; do not change Gemini.

## Parallel group A: T1, T2 (no shared files)

### T1 Real-agy probe (settles F5, gates D5) — agent: sdd-implementer — depends-on: none
- Files: `scripts/probe-agy-admin.sh`, `scripts/testdata/agy-stub-gateway/` (tiny stub, any language already in the repo's toolchain: Go preferred), spec section `## T1 findings` (append only).
- `probe-agy-admin.sh <admin.json>`: builds/runs a throwaway container (docker, root inside), copies the local agy 1.4.7 install (`~/.local/agent-clis/bin/agy` and whatever it needs; find out) in, places the file at `/etc/antigravity/admin_settings.json` root:root 0644, starts the stub gateway (https with a self-signed CA named by `caCertPath`), runs `agy models` and one `agy -p` print-mode prompt with `AGY_LLM_GATEWAY_API_KEY` set, prints: exit codes, stderr head, the stub's received requests (path, headers). Removes the container on exit. No host writes outside the scratch/temp dir; no sudo on the host.
- Cases to run and record in the spec (`## T1 findings`, one line each: input → agy behavior): golden full file; `{}`; empty file; unparseable JSON; unknown top-level key; unknown nested key; `enabled:true` without key env; no Google sign-in present (does "admin controls not applicable" fire?).
- Tests: `bash -n scripts/probe-agy-admin.sh`; `shellcheck` if present. The probe itself is local-only (AC9).
- Constraint: if agy cannot reach the stub at all (e.g. requires Google auth before reading the admin file), stop and report with evidence; do not work around with `--dangerously-skip-permissions` or patched binaries.
- Fixture: write the golden file as `internal/agent/agy/testdata/admin_settings.golden.json` (only this file in that dir) with: gateway {enabled, url https, wireProtocol genai, customHeaders {"X-Cost-Center": "..."}}, caCertPath, models {defaultModelId, list of 2}. T3's golden test uses the same file.

### T2 Validate (D3) — agent: sdd-implementer — depends-on: none
- Files: `internal/agent/agy/managed/managed.go`, `managed_test.go`, `keys.go` (dated allowlist, comment with source URL + date 2026-10-26, agy 1.4.7).
- Interface: `func Validate(doc map[string]any) error`; `func ForAgent(agentName string, doc map[string]any) error` (nil for agents other than "agy"; mirrors `gemini/managed.ForAgent`).
- Rules: spec D3 exactly. Nil/empty doc valid.
- Tests (AC2): `TestValidateAcceptsTheGoldenFixture` (reads `../testdata/admin_settings.golden.json` if present, else an inline copy of the same shape: gateway {enabled true, url "https://gateway.example.com", wireProtocol "genai", customHeaders {"X-Cost-Center": "platform"}}, caCertPath "/etc/ssl/certs/example-ca.pem", models {defaultModelId "gemini-3.8-flash", list [{modelId "gemini-3.8-flash"}, {modelId "gemini-3.8-pro"}]}); table test `TestValidateRejects` with one row each: unknown top-level, unknown nested (gateway.foo, models.list[0].foo), gateway.apiKey (error names AGY_LLM_GATEWAY_API_KEY), headers Authorization / X-Api-Key / Cookie, wireProtocol "grpc", url http://, enabled true without url, relative caCertPath, defaultModelId not in list, empty modelId, wrong types (list not array, headers value not string). `TestForAgentIgnoresOtherAgents`.

## T3 Adapter (D1, D5–D7, D10) — agent: sdd-implementer — depends-on: T1 (D5 outcome), T2
- Files: `internal/agent/agy/agy.go`, `render.go`, `agy_test.go`, `render_test.go`, `inspect_test.go`.
- Interfaces: `const Name = "agy"`; `func SystemDir(goos string) string` (F1; windows via `agent.ProgramData()`); `const AdminFile = "admin_settings.json"`; `type Adapter struct{ Binary, SystemDir string }`; `New()`, `Name()`, `Locate(env)`, `Build(ctx, o)`, `Inspect(env) []agent.Finding`, `Render(bundle) (agent.Rendering, error)`.
- Render: `bundle.Compile("").Agent(Name).Managed` → Validate → indented JSON + newline, `{}` for none (or T1's outcome), mode 0644; notes for repo-scoped agy rules (reuse Gemini's `dropped` approach).
- Build: D6. Notes per D6. No files.
- Inspect: D7 cases, messages per spec ("admin file present, revision X").
- Tests: AC1 `TestRenderMatchesTheGoldenFiles` (golden = T1's fixture), `TestRenderWithNoAgyRulesWritesEmpty`, `TestSystemDirPerOS`; AC5 `TestRenderNotesTheRepoScopedRulesItDropped`; `TestRenderRejectsAnInvalidDocument`; AC7 `TestBuild...` (env merged, args in order, note, `launch.Files` empty); AC8 `TestInspect...` one per D7 case.

## Parallel group B: T4, T5 (no shared files)

### T4 Wiring + e2e — agent: sdd-implementer — depends-on: T3
- Files: `cmd/aw/main.go`, `cmd/aw-sync/main.go` (registry, usage line, opt-in default per D8), `internal/sync/paths.go`, `cmd/awd/main.go` (`managedValidator` adds `agymanaged.ForAgent`), `internal/policy` (agy `launch` rejection like Gemini's, `TestCompileAgyModelsListReplaces`), e2e tests in `cmd/awd`, `cmd/aw-sync`, `cmd/aw`.
- D8 contract: default enroll list = renderable adapters minus an explicit opt-in set `{agy}`; `--agents` accepts agy. Keep it one small named list, not a flag.
- Tests: AC3 `TestApplyRejectsAnAgyGatewayAPIKey`, `TestApplyRejectsALaunchDocumentForAgy`; AC4 `TestCompileAgyModelsListReplaces` (also customHeaders accumulate, later wins per name); AC6 extend `TestEnrollOnceStatusAndOutage` (enroll `--agents claude,codex,gemini,agy`, `--root agy=...`, admin file content + `.aw-revision`, survives outage); AC8 extend `TestDoctorReportsHowTheAgentIsGoverned` with an agy case; AC12 `TestEnrollDefaultLeavesAgyOut`.
- Run both `go test ./...` and `GOTOOLCHAIN=go1.27.3 go test ./...` (AC11).

### T5 Docs — agent: sdd-implementer-lite — depends-on: T3
- Files: `docs/site/how-it-works.md` (agy section), `docs/site/reference.md` (`agents.agy.managed` keys, D3 rules), `docs/site/write-first-policy.md` (agy example), `docs/site/threat-model.md` (design-limit line: agy permissions/MCP/hooks are user-editable), `deploy/aw-sync/README.md` (`--agents ...,agy` opt-in + overwrite warning), `README.md` agent list, Gemini deprecation line where Gemini is introduced.
- Content: AC10. Messages quoted must match T3's code strings. Gateway key: developers get `AGY_LLM_GATEWAY_API_KEY` from their own credential store; never in policy `env`.
- Check: `for f in README.md docs/site/*.md; do bash scripts/check-links.sh "$f" || exit 1; done && bash scripts/check-readme.sh`.

## Backlog

- Remove the Gemini adapter once no policy uses `gemini` (Gemini CLI EOL 2026-06-18).
- agy permissions/MCP/hooks if Google adds a system tier or a settings-path override.
- aw-sync: skip-with-note for a foreign admin file instead of opt-in (needs per-agent skip in the Renderer contract).
- Per-agent isolation of a failed write in aw-sync.
- macOS/Windows real-agy verification.
- One shared `agent.DroppedRepoRules` helper (codex, gemini, agy copies) (panel-design).
- aw-sync removes files of agents no longer enrolled (panel-correctness).
- README: say `aw agy` is a thin launcher (panel-design nit).
- Inspect test: match a specific Error message, not any (reviewer-lite nit). Header value check: TrimSpace, more schemes (Token, Digest) (panel-correctness nit).
- README:82 drop inner "(end of life)" in compare cell; how-it-works:90 move hooks-bullet parenthetical below list (panel-cdo nits).
- awd apply validates merged docs per subject for every agent, not just per rule (agy fixed by dropping cross-field checks).

## Ledger

PLAN c762fb9 spec approved (owner 2026-10-26: enforced-only, apiKey forbidden, aw agy passthrough, key agy, agy opt-in enroll)
USAGE start 42
T2 done 59bfbcf sdd-implementer Validate D3, Windows caCertPath accepted, defaultModelId non-empty
T2 review approve 59bfbcf sdd-reviewer should: untested Windows caCertPath + defaultModelId; nits type rows, header substring doc (T5)
T2 coord fb40e5d test rows Windows caCertPath (mutation-checked) + defaultModelId empty/non-string
T1 done 945c96c sdd-implementer probe 10 cases; {} ok (D5 settled); bad JSON ignored silently; unknown keys ignored; no-key = no auth header, not error (D7 reword)
COORD review approve fb40e5d sdd-reviewer-lite T2 test rows; nits lowercase drive, len guard
T1 review approve 945c96c sdd-reviewer re-ran golden probe; should D7 reword; nit ad-hoc cases not saved
T1 coord 30f1fd6 spec D5 settled, D7 no-key warn wording + unparseable note
T1 fix 30f1fd6 sdd-reviewer-lite fix-needed D5 'same as no file' unverified
T1 coord 2ed5b25 D5 = T1 evidence only, F5 settled
COORD review approve 2ed5b25 coordinator applied reviewer-lite's exact wording (2 lines)
T3 done 018d218 sdd-implementer adapter; golden compared as JSON; env note skips _API_KEY; bad file = single error
T3 review approve 018d218 sdd-reviewer nits: D7 _API_KEY wording, unreadable case untested
T3 coord d4a3486 D7 wording matches code (excludes _API_KEY)
T5 done 97fd9de sdd-implementer-lite 6 docs files; AC10 checks run by coordinator: pass; open: removing.md row, README 3-agent mentions
T4 done e8abe9f sdd-implementer wiring + e2e; launch reject pre-existing; agy sorts first; doctor e2e reads real /etc
T4 review approve e8abe9f sdd-reviewer should: doctor e2e vacuous (rename); nits launch msg wording, optIn var, JSON test loosened
T5 fix 97fd9de sdd-reviewer-lite fix-needed model IDs unsourced, README/index 3 agents, removing.md row, wrap
T4 coord ebbea68 rename doctor e2e TestDoctorListsAgyFindings (wiring only) + AC8 command
T5 fix 2b3f9bf sdd-reviewer-lite fix-needed how-it-works:98,112 still 3 agents
T5 coord e3bafe4 how-it-works compare table + bullet list four agents
USAGE end 66
COORD review approve e3bafe4 sdd-reviewer-lite ebbea68 + e3bafe4; nits wiring-only test, plan cites old test name
PANEL cdo e4f5b69 must=0 should=7 nit=2
SEAT panel-cdo pass e4f5b69 AC10 markdown only, no browser run
PANEL design e4f5b69 must=0 should=3 nit=7
PANEL correctness e4f5b69 must=1 should=4 nit=3
SEAT panel-correctness pass e4f5b69 AC9 caCertPath trust confounded (not claimed)
PANEL security e4f5b69 must=0 should=2 nit=1
DISMISSED design type-checks drop (owner: keep, merge-safe per-field, loud at apply)
DISMISSED design probe delete (owner: keep, label one-off)
TRIAGE fixed=20 dismissed=2 backlog=3
FIX wave 6a360c6 sdd-implementer 20 items: cross-field checks dropped, url/header secret rejects, empty list, Inspect warn, probe CA moved + rerun, docs CDO items
PANEL cdo a2936f6 must=0 should=0 nit=2
SEAT panel-cdo pass a2936f6 AC10 re-run on fix diff
PANEL correctness a2936f6 must=0 should=1 nit=1
SEAT panel-correctness pass a2936f6 AC9 re-run on fix diff
COORD f06608d probe copies unparseable/no-gateway inputs raw (tested), spec list[] wording; Gemini retired-for-most-users + source link
COORD fix-wave re-review a2936f6 sdd-reviewer-lite fail: must Gemini EOL claim unsourced (fixed f06608d); nit weak Inspect Error match (backlog)
