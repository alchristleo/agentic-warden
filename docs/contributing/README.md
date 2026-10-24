# Contributing

Moved from the README. User-facing docs are in [docs/site](../site/index.md).

## Layout

    cmd/aw/           wrapper CLI: run an agent with the bundle compiled for its repository, doctor, agents
    cmd/aw-policy/    the policyHelper executable, offline
    cmd/aw-sync/      root-side sync: enroll, render every agent's files
    cmd/awd/          control plane: serve, apply
    deploy/           managed-settings install templates
    examples/         policies the docs and tests apply (quickstart.yaml, org-policy.yaml, groups.yaml)
    docs/site/        the pages served at /docs; docs/tapes/ and docs/assets/ hold the GIF recordings
    scripts/          check-quickstart.sh, check-links.sh and their tests
    web/              the marketing site and the /docs renderer
    internal/agent/   Adapter interface, registry, Prepare/Launch/Exec
    internal/agent/claude/schema/  vendored settings schema and validation
    internal/agent/codex/  Codex adapter: requirements.toml renderer and key allowlist; -c overrides for aw codex
    internal/agent/gemini/  Gemini adapter: settings.json and admin policy renderer, key allowlist
    internal/policyhelper/  what aw-policy does: read the bundle, compile, validate, emit
    internal/sync/    the sync cycle: fetch the bundle, render, write all-or-nothing
    internal/policy/  rules, targeting, compilation, YAML/JSON authoring
    internal/repo/    which repository a directory is in, from .git/config
    internal/merge/   settings deep-merge and environment merge
    internal/decide/  tool-call decisions; the seam for a semantic decider
    internal/store/   policy revisions (in-memory and Postgres)
    internal/handler/ the HTTP layer
    internal/glob     wildcard matching for rules and repo targeting
    internal/cache/   content-addressed generated files
    internal/config/  control plane configuration
    internal/credential/  minting and hashing machine credentials

## Tests

    go test ./...

No test runs a real agent: fakes are executable files placed on a temporary
PATH. No test needs a network.

The Postgres store runs the same conformance suite as the in-memory one. It
needs a throwaway database and skips without one:

    AWD_TEST_DATABASE_URL=postgres://... go test ./internal/store/

## Docs

Pages live in `docs/site/*.md` with `title`, `section`, `order` and `description` frontmatter. Link
pages as `./page.md` and images as `../assets/x.gif`; the site rewrites both. The quickstart is run
for real:

    scripts/check-quickstart.sh docs/site/quickstart.md
    scripts/check-links.sh docs/site/quickstart.md
    scripts/test-checks.sh

The first command needs the quickstart's prerequisites and a clone it can reach at
`https://github.com/alchristleo/agentic-warden.git`. While you work on a branch, point that URL at
your checkout with `url.<path>.insteadOf` in git's config. Blocks marked
`<!-- check: skip (needs a logged-in Claude) -->` are printed, not run.

## Marketing site

`web/` is the marketing site: a Next.js project with its own pnpm lockfile,
deployed to Vercel with `web/` as the project root. It does not share code
with the Go module.

    cd web
    pnpm install
    pnpm dev            # http://localhost:3000
    pnpm test           # unit tests
    pnpm test:e2e       # Playwright against a production build

Production deploys (`VERCEL_ENV=production`) need `NEXT_PUBLIC_SITE_URL`,
`RESEND_API_KEY`, `DEMO_INBOX` and `DEMO_FROM`; the build fails without
them. See `web/.env.example`.

## Maintenance

A weekly drift job checks the Codex requirements keys, the Gemini settings keys and decisions, and the Claude nested settings keys against upstream. Run it with `go test -tags drift ./internal/drift/`; keys we skip on purpose are in `claudeUnsupported`.
On drift, edit `internal/agent/codex/requirements/keys.go`, `internal/agent/gemini/managed/keys.go` or `internal/agent/claude/schema/overlay.go`.
GitHub disables scheduled workflows after 60 days of repository inactivity.
