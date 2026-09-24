# Launch-time Codex and Gemini adapters: per-repo overlays from the machine bundle

Date: 2026-09-22. Status: implemented 2026-09-22 (main 27e7c67). Extends
`2026-09-21-multi-agent-bundle-sync-design.md`, whose "Out of scope" listed
this as a later milestone.

## Why this exists

After M4e every agent's enforced, machine-wide configuration is a root-owned
file aw-sync writes. What a static file cannot carry is a repo-scoped rule:
"in the payments repository, Codex runs read-only" or "in payments, Gemini
may not call `run_shell_command`". For Claude those rules reach the agent
through the policy helper, which compiles the bundle per session. Codex and
Gemini have no helper; their renderers drop repo-scoped rules with a note.

The wrapper is the other place a per-session computation can happen. `aw
codex` and `aw gemini` compile the machine bundle for the repository the
session runs in and apply the result through each agent's launch-time
channel. This layer is advisory: a developer who runs bare `codex` or
`gemini` gets the machine-wide files and nothing more. That is the same
standing `aw claude`'s launch-time environment injection has today, and the
parent design accepted it when it deferred this milestone.

Gemini's system settings path can be moved with
`GEMINI_CLI_SYSTEM_SETTINGS_PATH`; Gemini's own enterprise documentation
recommends a wrapper that pins the variable. `aw gemini` is that wrapper.

## What `aw` compiles from

Today `aw` applies a pre-compiled `policy.Document` named by `--policy` or
`$AW_POLICY`. A per-repo overlay needs the rules with their matchers still
in them, which is the bundle.

`aw-sync once` writes one more file on every successful cycle:
`<state dir>/aw-bundle.json` (0644; `/var/lib/agent-wrapper` on Linux, the
existing `sync.StateDir` elsewhere). It is the fetched bundle with every
agent's rules and their matchers intact (Claude's own copy under
`/etc/claude-code` is narrowed to Claude's rules), placed where a machine
that enrols no Claude still has it. It is a planned
file like any rendered file: hashed into `state.json`, reported by `aw-sync
status`, overwritten on drift, and it gets the `.aw-revision` sibling every
`.json` file gets.

`aw` resolves its policy in this order:

1. `--policy PATH` or `$AW_POLICY`: load the Document as today. An explicit
   document is the developer's or an integrator's override and is used as
   given.
2. Otherwise `<state dir>/aw-bundle.json` (`$AW_SYNC_STATE_DIR` overrides
   the directory, as doctor already honours): `repo.Detect(cwd)`, then
   `bundle.Compile(repo)`. An unreadable or unparseable bundle is an error
   for `aw <agent>`, as an unreadable `--policy` is today: launching without
   the organization's configuration when one was meant to apply is worse
   than refusing.
3. No bundle file: launch with no settings. The launch carries the note
   `no policy: aw-sync has not written <path>`; `aw doctor` shows it.

`aw doctor` reports the source as `policy: <path> (document)`,
`policy: <path> (bundle <version>, repo <url or none>)` or `policy: none`.

## Policy: the `launch` document

`policy.AgentConfig` gains a fourth field:

```go
// Launch is the document the agent's wrapper applies per launch, in the
// agent's launch-time schema, for an agent whose launch channel takes a
// different shape from its managed file. Codex's requirements.toml and
// its config.toml are two schemas; Launch is the second. Claude and
// Gemini apply Managed at launch and have no use for it.
Launch map[string]any `json:"launch,omitempty"`
```

Across matching rules `Launch` deep-merges with `merge.Rules{}`: tables by
key, scalars and lists replace. `Document.Agent` returns it with the rest;
`agent.Settings` gains the same field so adapters receive it.

Validation at `awd apply` and `awd serve`, on the same path as the managed
validators: `launch` is accepted for `codex` only and must be an object.
Any other agent with a `launch` entry fails with `rule "x", agent "gemini":
launch is not supported; gemini applies managed at launch`. Codex's
`launch` keys are not allowlisted in this milestone: config.toml has many
keys that change often, and a wrong one is an error Codex itself prints at
startup for the developer to see, unlike a wrong requirements key, which
Codex ignores silently.

## `aw codex`

`Build`:

1. `Locate` as today.
2. Flatten `Settings.Launch` to dotted keys (`sandbox_mode`,
   `mcp_servers.docs.command`), sorted, and emit one `-c key=value` pair
   per leaf, before the developer's arguments. The value is TOML: strings
   quoted as TOML basic strings, booleans bare, whole numbers without a
   fraction (they arrive as `float64` from JSON), other numbers as written,
   arrays and inline tables as inline TOML. Codex parses the value as TOML
   and falls back to a raw string, so quoting strings keeps `"1.0"` a
   string.
3. Environment merged as today.
4. `Launch.Notes` gains one line per override, `codex: -c sandbox_mode =
   "read-only" from launch`, and the existing note that managed settings
   are enforced by `requirements.toml` stays.

The developer's arguments come last, so an explicit `-c` on the command
line wins over the policy's, as an explicit flag does for Claude. Advisory
means exactly that; the machine-wide `requirements.toml` is what bounds
what any `-c` can do.

## `aw gemini`

`Build`:

1. `Locate` as today.
2. Take `Settings.Managed`, which `aw` has already compiled for this
   repository, and split it with `managed.Parts`. Validate with
   `managed.Validate`; a failure is a launch error, since the bundle was
   validated at apply time and this can only mean a newer schema in this
   binary.
3. If `policies` is non-empty, encode them as the same `[[rule]]` TOML the
   renderer writes (header included) and store the bytes in the aw cache
   for gemini (`cache.Dir("gemini")`, `cache.Write` with prefix `policies`
   and extension `.toml`). Append the file's path to `settings.policyPaths`
   (creating the list; an author's own entries stay first).
4. Encode `settings` as the renderer does (`json.MarshalIndent`, trailing
   newline) and store it in the cache with prefix `settings` and extension
   `.json`. Prune the cache directory to the same 30-day age Claude uses.
5. Set `GEMINI_CLI_SYSTEM_SETTINGS_PATH=<cache settings path>` in the
   launch environment, forced: if the developer's environment already had
   it, note `gemini: GEMINI_CLI_SYSTEM_SETTINGS_PATH was <old>; replaced
   with the organization's settings for this session`. Then merge the
   policy's `env` as today; a policy that sets the same variable loses to
   the pin, with a note, because the pin is the point of the wrapper.
6. `Launch.Files` lists both cache files. Which repository the document
   was compiled for is `aw`'s knowledge, not the adapter's: `aw` appends
   `policy compiled for <repo or "no repository">` to every launch's notes.

Both files are rendered from the compiled document, so `aw gemini` in no
repository produces the same settings the machine-wide file holds, plus
`policyPaths` if there are policies. A launch in the payments repository
adds what the payments rule says.

Tier limitation, documented in the README: rules reached through
`policyPaths` load at Gemini's **user** tier. The admin tier's supplemental
paths (`adminPolicyPaths`, `--admin-policy`) are ignored whenever the
standard admin directory holds any `.toml`, which it always does once
aw-sync has run. A machine-wide admin rule that names a tool therefore
outranks a repo-scoped rule for the same tool. The guidance for authors:
machine-wide `policies` are the loose baseline and should name only what
must hold everywhere; repo-scoped `policies` tighten tools the baseline
does not name.

## Registry and doctor

`cmd/aw` registers `claude`, `codex` and `gemini`; `aw agents` lists all
three. Both new adapters implement `agent.Inspector`:

- Codex: `requirements.toml` at the system root — OK with its header
  revision, Warn when missing (`no requirements.toml: Codex is not governed
  on this machine until aw-sync has run`), Error when present but not TOML.
- Gemini: `settings.json` and `policies/50-agent-wrapper.toml` — the same
  three states each. Plus Warn when `GEMINI_CLI_SYSTEM_SETTINGS_PATH` is set
  in the developer's environment: bare `gemini` would read that path, not
  the system file (aw gemini overrides it, so the launch is fine).

Each adapter owns its system root, as Claude does with `claude.SystemDir`:
`codex.SystemDir(goos)` and `gemini.SystemDir(goos)` return the rows now in
`sync/paths.go` (`%ProgramData%` resolved from the environment at call
time, `C:\ProgramData` when unset). `sync.AgentRoot` keeps its signature
and delegates to the three adapters' functions, so the table has one owner
per row and `internal/sync` keeps importing adapters rather than the other
way round, which the parent design's doctor split requires. Both adapters
get a `SystemDir string` field, as Claude has, so tests aim `Inspect` at a
temporary directory.

## Failure modes

| Where | Failure | Behaviour |
| --- | --- | --- |
| aw-sync | writing `<state dir>/aw-bundle.json` fails | the cycle fails as for any planned file: nothing else is written, error recorded, exit 1 |
| aw | `--policy` set and unreadable | error, no launch (unchanged) |
| aw | bundle present and unreadable or invalid JSON | error, no launch |
| aw | no bundle, no `--policy` | launch with no settings; note |
| aw | not in a repository | compile with repo `""`: machine-wide rules only |
| aw codex | `launch` value the encoder cannot express (null) | error, no launch; nulls are rejected at apply time so this is a defect guard |
| awd | `launch` key not a bare TOML key (only letters, digits, `_`, `-`) | 422 / apply exits non-zero, naming the rule, agent and path |
| aw codex | stored policy has a non-bare `launch` key (from before this rule existed) | error, no launch; it used to pass a misparsed `-c` instead |
| aw gemini | managed fails `managed.Validate` | error, no launch |
| aw gemini | cache directory unwritable | error, no launch, naming the directory |
| awd | `launch` on an agent other than codex | 422 / apply exits non-zero, naming the rule and agent |

## Testing

- `policy`: `launch` merges by key across rules; `Document.Agent` returns it;
  `RuleSet.Validate` rejects `launch` for a non-Codex agent through the
  composed validator in `awd` (e2e: apply exits non-zero and the server
  returns 422).
- `codex`: flatten and encode — nested tables, arrays, strings with quotes,
  whole floats; `Build` emits sorted `-c` pairs before the developer's
  arguments; a null leaf is an error; `Inspect` three states.
- `gemini`: `Build` writes both cache files with the expected content, sets
  the pinned variable, notes an overridden developer value, lists the
  files; no policies → no TOML file and no `policyPaths`; `Inspect` three
  states plus the environment warning.
- `sync`: `once` writes `<state dir>/aw-bundle.json` and records its hash;
  `status` reports drift on it.
- `cmd/aw` e2e: with `AW_SYNC_STATE_DIR` pointing at a directory holding a
  bundle and a fake `codex` on PATH, `aw doctor --json` shows the Codex
  launch with `-c` pairs from a rule scoped to the repository the test runs
  in (a temporary git repository with an `origin`), and `aw agents` lists
  three agents; with no bundle, doctor shows `policy: none` and the note.
- Cross-builds for darwin and windows.

## Out of scope, deliberately

- A `launch` allowlist for Codex config keys. Codex reports a wrong key at
  startup; revisit if that proves too late in practice.
- Passing repo-scoped Gemini policies at the admin tier. Gemini's guard
  makes that impossible while the standard directory is populated, which
  aw-sync's design requires.
- Making the launch-time layer enforced. That is the policy helper's role
  for Claude and has no equivalent in Codex or Gemini today; the parent
  design's "advisory, bypassable" stands.
- Any change to the renderers or to `aw-policy`.
