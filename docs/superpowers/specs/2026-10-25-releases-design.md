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

Branch `releases`, base `612c69d`, head `8463db0`. Runs: AC1 `git grep` and `go test -count=1`; AC2 `go test -run Version -v` plus manual `-X` builds; AC3 `release-build.sh v0.0.0-test <tmp>/dist` then `test-release-build.sh <tmp>/dist v0.0.0-test`; AC4/5 `test-install.sh <tmp>/dist`; AC10 `check-quickstart.sh`. Temp dirs removed.

| # | criterion (short) | verdict | evidence (command + key output line) |
|---|---|---|---|
| 1 | No old module path; build and test pass | VERIFIED | `git grep -n "github.com/acme/agent-wrapper" -- ':!docs/superpowers'` printed nothing (exit 1); `go build ./... && go test -count=1 ./...`, 0 FAIL lines |
| 2 | Version stamping (4 binaries, `dev` when unstamped) | VERIFIED | `go test ./internal/version/ ./cmd/... -run Version -v` PASS (TestVersionStampedWins, TestVersionCommandStamped/Unstamped in aw, aw-policy, aw-sync, awd). Uncached manual build `-X ...internal/version.Version=v9.9.9`: `aw version` → `v9.9.9`, `awd version` → `v9.9.9`, `aw-sync version` → `v9.9.9`, `aw-policy build-info` → `version v9.9.9`. Unstamped: `dev` for all |
| 3 | Release archives, layout, arch, checksums, no overrides | VERIFIED | `release-build.sh v0.0.0-test <tmp>/dist`: 4 `.tar.gz` + `checksums.txt`; `tar -tzf` lists `aw aw-policy aw-sync awd LICENSE` in each; `file` on all 16 binaries: ELF x86-64 / ARM aarch64 for linux, Mach-O x86_64 / arm64 for darwin; `sha256sum -c checksums.txt` → 4× OK; `test-release-build.sh <tmp>/dist v0.0.0-test` → `ok: release build`, exit 0; linux/amd64 `aw-policy build-info` → `env-overrides=off`, `version v0.0.0-test` |
| 4 | Install happy path (default dir, AW_BIN_DIR, no root) | VERIFIED | `test-install.sh <tmp>/dist` → `ok default dir (latest)`, `ok custom dir, pinned`, `PASS`, exit 0. Script asserts 4 executables in each dir, `aw version` equals the dist version, PATH hint, run under `dash` |
| 5 | Install fails closed (tamper, missing sum, uname, 404) | VERIFIED | same run → `ok tampered`, `ok nosum`, `ok uname`, `ok notfound` (curl 404 → `download failed: ... (does AW_VERSION=v9.9.9 exist?)`), `ok staging`, `ok signal`. Each asserts non-zero exit, message match, and byte-identical `AW_BIN_DIR` (find + sha256 before/after) |
| 6 | POSIX shell: shellcheck clean; test runs under dash | VERIFIED | PR #6 CI `dry-run` pass (run 40178937558) includes `shellcheck -s sh install.sh`; `test-install.sh` runs install.sh under dash (PASS locally and in the same job) |
| 7 | PR CI dry-run job green | VERIFIED | `gh pr checks 6` → `dry-run pass 1m59s`, docs and test pass, publish skipping (tag-only) |
| 8 | Workflow security (permissions, SHA pins, no pull_request_target, quoting) | VERIFIED | `check: seat panel-security`: `SEAT panel-security pass 1aba8e6 AC8` (plan ledger line 118). Last commit touching `.github/workflows/release.yml` is 4367d75, an ancestor of 1aba8e6 (`git merge-base --is-ancestor`). Supporting: `grep -c pull_request_target release.yml` → 0 |
| 9 | First release assets and attestation | VERIFIED | Shipped as **v0.1.1** (v0.1.0 publish failed in `go test`, no release; fixed by PR #7). `gh release view v0.1.1`: 4 archives, checksums.txt, install.sh, Latest. `gh attestation verify agentic-warden_v0.1.1_linux_amd64.tar.gz -R alchristleo/agentic-warden` (gh 2.103.2) passes, signer release.yml@refs/tags/v0.1.1, run 40182273496; install.sh and checksums.txt also verify; tampered file rc=1. Real `install.sh` from latest/download: `aw version` → v0.1.1, `aw-policy build-info` env-overrides=off |
| 10 | Quickstart step 1 and README switch to install.sh (phase 2) | NOT VERIFIED | `docs/site/quickstart.md` step 1 still reads `go build -o bin/ ./cmd/...`; README line 104 still `go build`. `check-quickstart.sh docs/site/quickstart.md` exits 0 on the current file, but the criterion requires the install.sh switch, which is T7 (phase 2, not started, by design) |
| 11 | Reference Install section reads right, usable alone | VERIFIED | `check: seat panel-cdo`: `SEAT panel-cdo pass 1aba8e6 AC11` (plan ledger line 119). Last commit touching `docs/site/reference.md` is 4367d75, an ancestor of 1aba8e6 |

### Deviations

- AC9 verdict: the launch note asked for NOT VERIFIED. The `check: owner at gate` rule gives INCONCLUSIVE with evidence `owner checks at merge gate`; the owner confirms after the `v0.1.0` push.
- AC6 and AC7 stay INCONCLUSIVE until PR CI runs shellcheck and the dry-run job.
- AC2 `go test` output was served from cache (`(cached)`); the verdict also rests on uncached manual `-X` builds.
- AC10 is phase 2 by design (spec Decision 10); NOT VERIFIED records the missing switch, not a regression.
- Historical specs and plans under `docs/superpowers/` keep the old module path by design (Decision 1).
- Panel nits and open items are in the plan Backlog, not in this milestone.
