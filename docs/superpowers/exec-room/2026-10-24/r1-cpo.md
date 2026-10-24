# Exec room r1: CPO, the evaluator's first run

Reader: a platform or security lead deciding a purchase, in one sitting (`[NEED: real session length]`).
Activation is "they could show it to their boss", not "they finished reading".

## 1. Aha moment

**Aha:** "I wrote a rule, and my own Claude Code refused to do it." Example: the agent tries to
read `.env` and the policy denies it.

**Aha+ (the purchase trigger):** "...and it still refuses when I type bare `claude`, and when the
control plane is down." The security lead buys this part: enforcement does not need the
developer's cooperation. Aha is the hook, aha+ is the proof.

**Signal:** the evaluator runs step 7 below (edit, re-sync, behaviour changes): the policy becomes *theirs*. No analytics today. `[NEED: a way to count quickstart completions, even a "did it work?" link at the end]`

## 2. Current path (README as it is today)

I ran the rootless part on 2026-10-24 (awd in memory, aw-sync with `--state-dir`, `aw doctor`).
The mechanics work. The README does not lead to them.

| # | Step the README asks for | Blocker |
|---|---|---|
| 0 | Read 214 lines of gap analysis, internals and signing before "Try it" | Value is buried. The reader leaves before the try |
| 1 | `git clone` (not stated) | No released binaries, no tags. Module path `github.com/acme/agent-wrapper` ≠ repo, so `go install` fails |
| 2 | `docker compose up` **or** `go build awd` + `awd serve` | Docker still needs a local Go build for the `awd` CLI. Two paths and no choice made |
| 3 | `export AWD_ADMIN_TOKEN` | Explained after the command that needs it |
| 4 | `awd apply examples/org-policy.yaml` | 3 rules, 3 agents, Gemini priorities: too much for a first rule |
| 5 | `awd enroll-token` | ok |
| 6 | `curl … /v1/machines/enroll` with hand-made JSON | **Obsolete:** `aw-sync enroll` already does this |
| 7 | `curl … /v1/bundle` with a `<credential>` placeholder | Dead end: the output is JSON, not an agent |
| 8–9 | Build `aw-sync` and `aw-policy -tags awtest` | A test-only build tag in the evaluator's path |
| 10–12 | Mint a token again, `aw-sync enroll`, `aw-sync once --root …` | The paths that `--root` and `AW_POLICY_BUNDLE` need |
| 13 | Run `aw-policy` | Prints JSON. Still no agent |
| 14–15 | Build `aw`, `aw --policy <(curl …) doctor` | doctor never runs the agent |

**Count:** about 15 commands, plus a clone and 2 to 4 builds. **The aha is never reached:** no
step runs Claude Code. Missing prerequisites: Go toolchain, Docker, and Claude Code installed and
logged in (that last one is never stated).

Also found: rootless `aw doctor` prints **6 warnings** on a healthy evaluation setup ("owned by uid 1000,
not root", ...). Each is true, but a security reader's first screen is red.

## 3. Proposed path (Get started, about 10 minutes, rootless, nothing written to /etc)

Prerequisites box: Linux or macOS, Claude Code installed and logged in. Docker and Go are not needed.

| # | Command (one per GIF) | You should see |
|---|---|---|
| 1 | `curl -fsSL <site>/install.sh \| sh` (aw, awd, aw-sync, aw-policy into `~/.local/bin`) | 4 binaries, versions printed |
| 2 | `export AWD_ADMIN_TOKEN=try-me; awd serve &` (in memory, evaluation only) | `listening on :8080` |
| 3 | `awd apply quickstart.yaml` (one rule: deny `Read(./.env)`) | `applied revision … (1 rule)` |
| 4 | `awd enroll-token you@acme.com \| aw-sync enroll --server … --state-dir ~/.aw-try` | `enrolled machine …` |
| 5 | `aw-sync once --state-dir ~/.aw-try` | `synced bundle …` |
| 6 | `cd demo-repo && AW_SYNC_STATE_DIR=~/.aw-try aw claude "read .env"` | **Claude refuses: denied by policy** (aha) |
| 7 | Add `Bash(curl:*)` to the deny list, `awd apply`, `aw-sync once`, ask again | New rule in force, no restart (makes it *theirs*) |

**7 steps, one command each (step 7 is a short loop).** Then a clear fork:
"**See it enforced on bare `claude`** (test VM, sudo, about 5 more minutes)": `sudo aw-sync enroll` +
`install-timer`, type plain `claude`, get denied. Stop `awd`, and still get denied. This is aha+.
It needs root and writes to `/etc/claude-code`, so it goes on a test VM. A laptop is the wrong
place. Say this up front. **Candour sells to this reader.**

Honest caveat on step 6, written on the page: `aw claude` passes the rules as `--settings`. The
guide in step 8 shows the managed-tier path that a developer cannot skip.

Hosted shortcut: a design partner with a cell skips steps 2–3 and gets a URL and a token.
The docs say "Request a sandbox cell" and point to /demo. They do not say "Sign up", because the
hosted console is not live.

## 4. Product changes needed (smallest set)

1. **Release binaries + `install.sh`** (goreleaser, linux/darwin amd64/arm64, checksums, and
   signed if cheap). Fix the module path to the real repo so `go install` also works. This
   removes the biggest blocker. M.
2. **`aw-sync enroll` reads the token from stdin** when it is piped (today: flag or
   `AW_SYNC_TOKEN`). This makes step 4 one line. S. *No new `aw enroll` command is needed:
   `aw-sync enroll` exists. The README just never used it.*
3. **`examples/quickstart.yaml`**: one Claude rule, comments in plain words. S.
4. **doctor shows the evaluation state as one line**: when `AW_SYNC_STATE_DIR` is user-owned,
   print `evaluation mode: rules applied via aw only; see "Enforce on bare claude"` in place of the
   6 warnings. This adds no flag. It keeps the full report under `--json`. S.
5. Optional, check first: `aw-sync uninstall` (or a documented cleanup) so a VM trial can be
   undone. Today only `uninstall-timer` exists. S.

Not building: `awd demo`, a one-shot script, or a Docker sandbox with Claude inside. Each hides steps the evaluator must repeat later.

## 5. Docs information architecture (`/docs` on web/; README links to it)

**README (≤60 lines):** name (Agentic Warden), one-liner, hero GIF (step 6), what it does in 3
bullets, the honest limits link, a 3-line "try it" pointing to /docs/quickstart, license.
Everything else moves out.

**Get started**
- `/docs` Overview: what it governs, the 3 parts (awd, aw-sync, aw-policy) on one diagram, who it is for.
- `/docs/quickstart`: the 7 steps above, copy buttons, a GIF and "you should see" per step.
- `/docs/enforce-bare-claude`: aha+ on a test VM: managed tier, offline, undo.
- `/docs/how-it-works`: the policy → signed bundle → managed settings → launch path, failure modes (exits 0).

**Guides** (each one is a single task with a single result)
- Write your first policy: rule order, deny/allow, the full example explained.
- Per-repository rules: repo globs, the `.git/config` caveat stated.
- Groups and your IdP: authored map, snapshot, SCIM for Okta/Entra.
- Roll out to a fleet: install aw-sync + aw-policy via MDM, the timer, `awd machines`.
- Codex and Gemini CLI: what is enforced vs advisory, `aw codex` / `aw gemini`.
- Sign bundles: seed key, rotation, AWS KMS (moved out of README).
- Admin console with OIDC.
- Self-host for production (Postgres, TLS) vs hosted cell (links trust.md, runbook stays internal).
- Security model and limits: client-side control, detection vs prevention, what to pair it with.
  **This is a selling page for this reader and should sit high in the nav.**

**Reference**: `awd`, `aw-sync`, `aw`, `aw-policy` commands; environment variables; policy
schema; files written per OS; HTTP API. Generate from `--help` where possible so it stays
correct.

**Out of /docs, into repo `docs/contributing/`:** layout, tests, drift job, web/ dev, internals.

## 6. GIF plan (vhs tapes in CI, 3–6 s each, no audio)

| GIF | Page | Shows |
|---|---|---|
| Hero | README + /docs + home | Step 6: prompt, then Claude refuses, with the policy line on screen (≤8 s) |
| Q1–Q5 | quickstart, one per step | The command and its "you should see" output |
| Q6 | quickstart | Same as hero, in full |
| Q7 | quickstart | Edit YAML → apply → sync → new denial |
| E1 | enforce-bare-claude | Bare `claude` denied, then `awd` stopped, still denied |
| D1 | security model | `aw doctor` verified signature line |

Risk: agent GIFs need a logged-in Claude in CI. `[NEED: CTO call: record agent GIFs by hand, script CLI ones]`.
Denial text must be a real capture, never mocked. `[NEED: exact denial wording from a real run]`

## 7. What to cut

- The gap analysis at the top of the README. A one-line "why" stays; the comparison moves to /product.
- The curl enroll, the curl bundle and the `-tags awtest` walkthrough. They are wrong for evaluators.
- Docker as the default path. Keep it in the self-host guide only.
- The 3-agent, 3-rule example as the first policy.
- Signing, KMS, console env tables, layout, tests, drift and maintenance: all out of the README.
- Any page that mixes "what it is" with "how we build it".

## Top 5 proposals

| # | proposal | why it sells | effort guess S/M/L | depends on |
|---|---|---|---|---|
| 1 | Rootless 7-step quickstart ending in "Claude refused to read .env" + edit-and-reapply loop | Evaluator sees their own rule block their own agent in about 10 min; today the README never runs an agent | M | 2, 3 |
| 2 | Release binaries + one-line installer; fix module path | Removes the Go/Docker/clone blocker, the biggest drop-off point | M | none |
| 3 | Small CLI polish: `quickstart.yaml`, `aw-sync enroll` token from stdin, doctor "evaluation mode" line | Each step is one copyable line; first screen is not 6 red warnings | S | none |
| 4 | "Enforce on bare claude" page + "Security model and limits" page high in nav | The purchase trigger for a security lead: bypass-resistance and offline, with limits stated plainly | S | 1 |
| 5 | README cut to ≤60 lines with hero GIF; /docs IA as above; internals to docs/contributing | First screen shows value, not a competitor table; one source of truth at /docs | M | 1 (GIFs) |
