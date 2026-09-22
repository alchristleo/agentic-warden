# aw-policy: environment overrides compiled out of release builds

Date: 2026-09-22. Status: approved design, not yet implemented. Amends the
"aw-policy, offline" section of
`2026-09-21-multi-agent-bundle-sync-design.md`.

## Why this exists

`aw-policy` is the enforcement path: Claude Code runs it at startup and
applies what it prints above every other settings source. It reads the
bundle from the system directory (`/etc/claude-code/aw-bundle.json` and its
macOS and Windows equivalents) — unless `AW_POLICY_BUNDLE` names another
file, and it reads its configuration from `<system dir>/aw-policy.json` —
unless `AW_POLICY_CONFIG` names another.

Both variables exist for tests. But Claude Code hands the helper the
developer's environment, so a developer who exports `AW_POLICY_BUNDLE` in
their shell runs on a bundle they wrote. The README has said since M4c that
the guarantee is "against accident, not intent". This design closes it.

Threat model, stated so the boundary is clear:

- In scope: a developer without root on their own machine, able to set any
  environment variable and to write any file they own.
- Out of scope: root. Root can replace the binary, the drop-in or the bundle
  and no check in the helper changes that. The deploy documentation already
  requires the binary, the drop-in and the bundle to be root-owned.
- Out of scope, still: repo-scoped rules key on `.git/config`'s `origin`,
  which the developer can edit. That is a property of "which repository is
  this" having no trusted answer on a developer machine; it stays in the
  README as the remaining caveat.

## Design

### The gate is a build tag

`cmd/aw-policy` gains two files, one compiled in each configuration:

- `env_release.go` — `//go:build !awtest` — `const envOverrides = false`
- `env_awtest.go` — `//go:build awtest` — `const envOverrides = true`

`loadConfig` takes `allowEnv bool` and `main` passes `envOverrides`. When
`allowEnv` is false the two variables are never consulted; the bundle path
is `<system dir>/aw-bundle.json` and the configuration path is
`<system dir>/aw-policy.json`, full stop. If either variable is nonetheless
set, the helper adds a note to stderr:

    AW_POLICY_BUNDLE is set but this build ignores it; the bundle is read from /etc/claude-code only

(and the same for `AW_POLICY_CONFIG`). The note is for the developer who
set it, so they learn the override is inert instead of wondering why their
file has no effect. It is not a security event: nothing was bypassed.

Why a tag and not an ownership check: an ownership check would have to be
written twice (uid on POSIX, ACLs on Windows), would be the weaker of the
two on Windows, and would leave tests unable to use the override without
running as root. A tag makes the release binary ignore the variables
outright, tests opt in explicitly, and a packager who forgets the tag gets
the safe binary. The tag is spelled `awtest` so that it reads as
test-only in a build command.

### Detection: `aw-policy build-info` and `aw doctor`

`aw-policy build-info` prints one line to stdout and exits 0:

    env-overrides=off

or `on` for a tagged build. `build-info` is the only argument the helper
recognises. Any other argument, or none, runs the helper as before: Claude
Code passes no arguments today, and if a future version passed one, the
helper must still emit settings rather than fail the launch.

`aw doctor`'s Claude inspection, after `checkHelperBinary` finds the helper
present, regular and executable, runs `<helper> build-info` with a 2 second
timeout. On stdout containing `env-overrides=on` it adds a Warn finding:

    policyHelper /usr/local/bin/aw-policy was built with -tags awtest: a developer can point it at their own bundle; rebuild without the tag

Any other outcome — the process fails, times out, or prints something else —
adds no finding: the drop-in may name a helper that is not ours, and a
helper that errors on an unknown argument is not evidence of anything. Only
a positive match warns.

### Tests

- Both e2e `TestMain`s that build `aw-policy` (`cmd/aw-policy/e2e_test.go`,
  `cmd/aw-sync/e2e_test.go`) build with `-tags awtest`. Every existing test
  that sets `AW_POLICY_BUNDLE` or `AW_POLICY_CONFIG` keeps working unchanged.
- `cmd/aw-policy/e2e_test.go` builds a second binary without the tag, in the
  same `TestMain`, and adds:
  - release build with `AW_POLICY_BUNDLE` pointing at a valid fixture emits
    `{}` (the system path has no bundle on a test machine) and stderr
    contains the "ignores it" note naming the variable;
  - release build `build-info` prints `env-overrides=off`; tagged build
    prints `env-overrides=on`;
  - release build with an unrecognised argument still emits a complete
    envelope and exits 0.
- Unit tests in `cmd/aw-policy` for `loadConfig(getenv, false)`: the bundle
  path is the system path whatever `getenv` returns, and the note is
  present only when the variable is set.
- Unit test in `internal/agent/claude` for the doctor finding, with a fake
  helper script that prints `env-overrides=on`, one that prints `off`, and
  one that exits 1; only the first produces the Warn.
- `GOOS=darwin` and `GOOS=windows` builds, with and without the tag.

### Documentation

- `README.md`, "How policy reaches the agent": the caveat paragraph becomes
  a statement that release builds ignore the two variables (they exist only
  under `-tags awtest`, for tests), that `aw doctor` warns if a tagged
  helper is installed, and that the remaining caveat is `.git/config`'s
  origin for repo-scoped rules.
- `deploy/managed-settings/README.md`: the build step is a plain
  `go build ./cmd/aw-policy`; never install a binary built with
  `-tags awtest`; `aw doctor` reports one.
- `2026-09-21-multi-agent-bundle-sync-design.md`, "aw-policy, offline",
  step 1: the sentence "`AW_POLICY_BUNDLE` overrides the path for tests"
  gains "(test builds only; see the 2026-09-22 hardening design)".

## Failure modes

| Where | Failure | Behaviour |
| --- | --- | --- |
| aw-policy (release) | `AW_POLICY_BUNDLE` or `AW_POLICY_CONFIG` set | ignored; note on stderr; system paths used |
| aw-policy | unrecognised argument | runs normally, emits the envelope |
| aw doctor | helper fails, times out or prints something unexpected on `build-info` | no finding |
| aw doctor | helper prints `env-overrides=on` | Warn naming the helper and the fix |

## Out of scope, deliberately

- Verifying the helper binary's ownership or hash from doctor. Root owns
  the install; a hash check would need a trusted source for the hash, which
  is the signed-bundles question, deferred in the parent design.
- Any change to `policyhelper.Run`, the envelope, the audit log or the
  configuration file's keys.
