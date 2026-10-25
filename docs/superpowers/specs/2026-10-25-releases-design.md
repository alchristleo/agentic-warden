# Releases: binaries, install.sh, module path

## Goal

A new user installs the four binaries with one command, without Go. A pushed `v*` tag
builds, checksums and attests a GitHub release. The module path matches the repository.

## Non-goals

- GoReleaser, Homebrew tap, apt/winget packages, self-update (Backlog).
- Container image publishing to GHCR or ECR (Backlog; compose keeps building locally).
- Windows binaries (the Windows install-timer path has never run on a real host).
- Reproducible-build byte equality (attestation covers provenance).
- Publishing anything during the build. The first `v0.1.0` tag is the owner's push.

## Decisions

1. **Module path:** `github.com/alchristleo/agentic-warden`, the GitHub remote. Shapes
   considered: the remote path, or a vanity domain (no domain exists; it would also add a
   redirect to run). Example org names in policies (`github.com/acme/payments*`,
   `alice@acme.com`) stay: they are sample data, not the module path. Historical
   specs and plans under `docs/superpowers/` keep the old path.
2. **Integrity (owner):** SHA-256 checksums checked by `install.sh`, plus GitHub build
   provenance (`actions/attest-build-provenance`) on every archive, checkable with
   `gh attestation verify`. No signing keys to manage.
3. **Platforms (owner):** linux and darwin, amd64 and arm64. 4 archives.
4. **Install dir (owner):** `install.sh` installs to `~/.local/bin` (no root) unless
   `AW_BIN_DIR` is set. Enforcement on a real machine needs a root-owned `aw-policy`:
   `curl ... | sudo env AW_BIN_DIR=/usr/local/bin sh`.
5. **Release tooling:** one script, `scripts/release-build.sh <version> <outdir>`, called
   by `.github/workflows/release.yml`. Shapes considered: GoReleaser (one more tool and
   config format for 4 binaries × 4 targets) or a script (runs the same locally and in CI).
   Script chosen.
6. **Archive format (one-way door, users' scripts parse it):**
   `agentic-warden_<version>_<os>_<arch>.tar.gz` holding `aw aw-policy aw-sync awd LICENSE`
   at the top level, plus `checksums.txt` in `sha256sum` format. Shapes considered: one
   archive per target (chosen: one download, one checksum line) or one file per binary
   (16 downloads, 16 lines).
7. **Version:** package `internal/version`, `Version` set by `-ldflags -X`. Unset →
   the module version from `debug.ReadBuildInfo` (so `go install ...@v0.1.0` reports
   `v0.1.0`) → else `dev`. `aw version`, `awd version`, `aw-sync version` print it;
   `aw-policy build-info` adds a `version` line (it must not gain new arguments: any other
   argument runs the helper).
8. **Release builds are plain builds:** `CGO_ENABLED=0 -trimpath -ldflags "-s -w -X ..."`,
   never `-tags awtest`, so the shipped `aw-policy` ignores the env overrides.
9. **install.sh knobs:** `AW_VERSION` (default: latest) and `AW_BIN_DIR`. `AW_BASE_URL`
   (default `https://github.com/alchristleo/agentic-warden/releases`) exists for mirrors
   and tests and is documented in Reference only. POSIX `sh`, needs `curl`, `tar` and
   `sha256sum` or `shasum`.
10. **Two phases, one milestone.** The quickstart cannot point at a release that does not
    exist yet, and docs CI runs it on every PR. Phase 1 (this branch, merge gate): rename,
    version, release script, workflow, install.sh, Reference docs; quickstart unchanged.
    Then the owner pushes `v0.1.0`. Phase 2 (a small follow-up PR under this spec):
    quickstart step 1 and the README switch to `install.sh`; docs CI checks them against the
    real release.

## Design

- `install.sh` (repo root, also uploaded as a release asset): detect OS/arch from
  `uname -s`/`uname -m` (`x86_64|amd64` → amd64, `aarch64|arm64` → arm64; anything else
  exits 1 naming the platform). Download the archive and `checksums.txt` into a temp dir,
  verify the archive's line, extract, then move the 4 binaries into `AW_BIN_DIR`
  (created if missing). Any failure exits non-zero before anything is written to
  `AW_BIN_DIR`. If `AW_BIN_DIR` is not on `PATH`, print the `export PATH=...` line.
  `latest` resolves through `<base>/latest/download/<file>`, no API call.
- `release.yml`: on `push: tags: ['v*']`: `go test ./...`, `release-build.sh "$tag" dist`,
  attest `dist/*.tar.gz`, `gh release create "$tag" dist/* install.sh --verify-tag
  --generate-notes`. On `pull_request` touching the release files: the same build with
  version `v0.0.0-ci`, then `scripts/test-install.sh dist` against a local HTTP server. No
  publish, no write permissions on the PR job. Third-party actions pinned by commit SHA.
- `scripts/test-install.sh <dist>`: serves `<dist>` + `install.sh` on 127.0.0.1, runs the
  happy path and the failure cases below with a temp HOME and `AW_BASE_URL`.
- `docs/site/reference.md` gains an Install section: the one-liner, the 3 env vars,
  `sha256sum -c` and `gh attestation verify` commands, the sudo form for enforcement, and
  `go install github.com/alchristleo/agentic-warden/cmd/...@latest` as the from-source path.

## Phase 2 decisions (owner, 2026-10-25)

- Quickstart step 1 keeps `git clone` (example policy, later `cd ../agentic-warden`) and replaces
  `go build` with `install.sh`; Go leaves the quickstart prerequisites (from-source path only).
- `check-quickstart.sh` installs the PR's own build: CI builds a dist, serves it locally, rewrites the
  doc's release base URL to it and sets `AW_BASE_URL`. Doc commands stay literal.
- Tape 01 re-recorded with the same local-dist redirect (no live network).
- Folded in: Install links from home and enforce-plain-claude.md (sudo form instead of `bin/`), and the
  6 panel-cdo Reference nits from the releases Backlog.

## Acceptance criteria

1. **No old module path.** `git grep -n "github.com/acme/agent-wrapper" -- ':!docs/superpowers'`
   prints nothing, and `go build ./... && go test ./...` passes.
2. **Version stamping.** A binary built with `-X .../internal/version.Version=v9.9.9` prints
   `v9.9.9` from `aw version`, `awd version`, `aw-sync version`, and a `version v9.9.9`
   line from `aw-policy build-info`; unstamped builds print `dev`:
   `go test ./internal/version/ ./cmd/...  -run Version -v`.
3. **Release archives.** `scripts/release-build.sh v0.0.0-test dist` writes exactly 4
   archives named per Decision 6, each holding the 4 binaries + LICENSE for its os/arch
   (checked with `file`), and `cd dist && sha256sum -c checksums.txt` passes. The linux/amd64
   `aw-policy build-info` reports the env overrides off:
   `scripts/test-release-build.sh dist v0.0.0-test` exits 0.
4. **Install, happy path.** Against a local served dist, `install.sh` with a temp HOME puts
   4 executables in `~/.local/bin` (default) and in a custom `AW_BIN_DIR`, without root, and
   `aw version` prints the dist's version: `scripts/test-install.sh dist` exits 0.
5. **Install fails closed.** A tampered archive, a missing checksum line, an unsupported
   `uname` (PATH shim), and an HTTP 404 each exit non-zero, and `AW_BIN_DIR` is unchanged
   (byte-identical listing before/after): the same script's failure cases.
6. **POSIX shell.** `shellcheck -s sh install.sh` is clean and `test-install.sh` runs
   `install.sh` under `dash`.
7. **PR CI.** The `release.yml` dry-run job is green on this branch's PR (`gh pr checks`).
8. **Workflow security.** Publish job alone has `contents: write`, `id-token: write`,
   `attestations: write`; PR job read-only; every third-party action pinned to a full
   commit SHA; no `pull_request_target`; tag value not interpolated into `run:` without
   quoting. `check: seat panel-security`.
9. **First release (after merge, owner pushes `v0.1.0`).** `gh release view v0.1.0` lists
   4 archives, `checksums.txt` and `install.sh`, and
   `gh attestation verify agentic-warden_v0.1.0_linux_amd64.tar.gz -R alchristleo/agentic-warden`
   passes. `check: owner at gate` for the tag push; the commands are run after it.
10. **Quickstart switch (phase 2).** Quickstart step 1 and the README install lines use
    `install.sh`, the Go requirement moves to the from-source path, and
    `scripts/check-quickstart.sh docs/site/quickstart.md` passes in docs CI on the phase-2 PR.
11. **Docs read right.** The Reference Install section states only what the scripts do,
    gives the sudo form for enforcement and both verify commands, and a new user can install
    from it alone. `check: seat panel-cdo`.

## Security notes

- Trust boundary: a script piped into a shell, and binaries that end up root-owned for
  enforcement. Threats: tampered download (checksum, fail closed), compromised release
  (attestation ties archives to this repo's workflow run at the tagged commit), a
  workflow that publishes from a PR (no write tokens on PR jobs, no `pull_request_target`),
  a mutable action tag swapped upstream (SHA pins), script injection via tag names
  (quoted, tags matched `v*`).
- Checksums come from the same release as the archive, so they detect corruption, not a
  compromised release; the attestation command is the check for that, and the docs say so.
- `install.sh` never calls `sudo` itself; root is the user's explicit choice.
- Release `aw-policy` is built without `awtest` (AC3 checks it).

## Status (converged 2026-10-25)

Phase 2 (quickstart switch, PR on branch `quickstart-switch`, head `2e8a63f` before this commit). Replaces the phase 1 table; phase 1 status is in git history before this commit.
Runs: fresh `scripts/release-build.sh v0.0.0-ci <tmp>/dist`; `go test -count=1 ./...`; `scripts/test-release-build.sh <tmp>/dist v0.0.0-ci`; `scripts/test-install.sh <tmp>/dist`; `scripts/test-checks.sh`; `AW_QS_DIST=<tmp>/dist scripts/check-quickstart.sh` on `docs/site/quickstart.md` and `README.md`. Temp dirs removed.

| # | criterion (short) | verdict | evidence (command + key output line) |
|---|---|---|---|
| 1 | No old module path; build and test pass | VERIFIED | `git grep -n "github.com/acme/agent-wrapper" -- ':!docs/superpowers'` exit 1, no output; `go build ./...` exit 0; `go test -count=1 ./...` exit 0, every package `ok` or `[no test files]` |
| 2 | Version stamping, `dev` when unstamped | VERIFIED | `go test ./internal/version/ ./cmd/... -run Version -v -count=1`: `--- PASS: TestVersionStampedWins`, `TestVersionCommandStamped`, `TestVersionCommandUnstamped`, `TestVersionDefaultDev`; `ok` for cmd/aw, cmd/aw-policy, cmd/aw-sync, cmd/awd, internal/version. Manual `-X` builds from phase 1 not re-run |
| 3 | Release archives, layout, arch, checksums, no overrides | VERIFIED | fresh `release-build.sh v0.0.0-ci`: 4 `.tar.gz` + `checksums.txt`; `tar -tzf` of each archive lists `aw aw-policy aw-sync awd LICENSE`; `file` on the binaries: ELF x86-64 / ARM aarch64 (linux), Mach-O x86_64 / arm64 (darwin); `sha256sum -c checksums.txt` → 4× `OK`; `scripts/test-release-build.sh <tmp>/dist v0.0.0-ci` → `ok: release build`, exit 0 |
| 4 | Install happy path (default dir, AW_BIN_DIR, no root) | VERIFIED | `scripts/test-install.sh <tmp>/dist` → `ok default dir (latest)`, `ok custom dir, pinned`, `PASS`, exit 0 |
| 5 | Install fails closed (tamper, missing sum, uname, 404) | VERIFIED | same run → `ok tampered`, `ok nosum`, `ok uname`, `ok notfound`, `ok staging`, `ok signal`; exit 0 |
| 6 | POSIX shell: shellcheck clean; test runs under dash | VERIFIED | PR #8 release `dry-run` job (run 40184621275) passed; it runs `shellcheck -s sh install.sh` and `scripts/test-install.sh dist`. Locally `test-install.sh` under dash passed (row 4); shellcheck is not installed locally |
| 7 | PR CI dry-run job green | VERIFIED | PR #8 checks at `698ac38`: `dry-run pass 1m30s` (run 40184621275); also docs, tapes and test pass |
| 8 | Workflow security (permissions, SHA pins, no pull_request_target, quoting) | VERIFIED | `check: seat panel-security`: `SEAT panel-security pass 1aba8e6 AC8` (plan ledger line 130). `git merge-base --is-ancestor 4367d75 1aba8e6` ok; `release.yml` not touched by this branch. Also `grep -c pull_request_target .github/workflows/release.yml` → 0; `contents: write`, `id-token: write`, `attestations: write` at lines 44-46 |
| 9 | First release assets and attestation | VERIFIED | `SEAT owner pass v0.1.1 AC9` (plan ledger): v0.1.1 release has 4 archives, `checksums.txt`, `install.sh`; real `curl \| sh` install, `sha256sum` OK, `gh attestation verify` passes for archive, install.sh and checksums.txt (run 40182273496); release.yml unchanged since |
| 10 | Quickstart switch to install.sh; Go to from-source; check-quickstart passes | VERIFIED | `AW_QS_DIST=<tmp>/dist scripts/check-quickstart.sh docs/site/quickstart.md` → `check-quickstart: docs/site/quickstart.md ok (10 run, 2 skipped, 24 expected lines found)`, exit 0; `... README.md` → `ok (4 run, 0 skipped, 8 expected lines found)`, exit 0. Step 1 is `curl ... install.sh \| sh` (quickstart.md line 24); README line 105 same; quickstart.md line 14 says Go is not needed. `.github/workflows/docs.yml` lines 64-72 run the same two commands with `AW_QS_DIST`. PR #8 docs jobs passed at `698ac38` (runs 40184618275, 40184621257) on the PR's own build |
| 11 | Reference Install section reads right, usable alone | VERIFIED | `check: seat panel-cdo`: `SEAT panel-cdo pass 2e8a63f AC11` (re-run on `git diff 1aba8e6..HEAD -- docs/site/reference.md`, after `428d1c9` and `24863b0`) |

### Deviations

- AC10: local run plus PR #8 docs CI, both on a fresh build of this checkout. The two skipped blocks (logged-in Claude) are skipped in CI too.
- AC3 was run with version `v0.0.0-ci`, not `v0.0.0-test` as the criterion writes. The version string is the only difference.
- AC2 `go test` was run with `-count=1`. Manual `-X` builds from phase 1 were not repeated.
- AC6, AC7, AC10 settled by PR #8 CI; AC9 by the owner on v0.1.1; AC11 by a scoped panel-cdo re-run (coordinator update after converge).
- Phase 2 panel findings: fixed in `7dffaf4`..`60cde48` (re-review approved); the no-release 404 finding was dismissed by the owner (v0.1.1 is Latest); remaining nits are in the plan Backlog.
