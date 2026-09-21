# agent-wrapper

Enterprise control plane for coding agents. An organization authors one policy;
each developer's session gets the slice of it that applies to them, targeted by
group and by repository.

Status: early. Claude Code is the only agent implemented.

## Why this exists

Claude Code already ships a lot of what an organization needs: managed settings
files, MDM delivery, server-managed settings from the claude.ai console,
`managedMcpServers`, sandboxing, and OpenTelemetry. Anthropic's self-hosted
Claude apps gateway adds SSO, per-IdP-group managed settings and spend limits
for organizations that bring their own API key or cloud provider.

What remains uncovered: **per-repository targeting**, per-group policy for
organizations on claude.ai seats (the console cannot target a group, and the
gateway needs an upstream credential), and any agent that is not Claude Code.
This project fills those gaps, on an agent-agnostic interface so other agents
can follow.

## How policy reaches the agent

Enforcement rides on `policyHelper`, a managed-only Claude Code setting naming
an executable that Claude Code runs at startup to compute the session's managed
settings. Its output becomes the top of the settings stack, above the command
line, the project and the user.

That matters because the helper runs on `claude`, not on `aw claude`. A
developer who bypasses the wrapper is still governed.

One caveat, deliberate for now: the helper honours `AW_POLICY_BUNDLE` and
`AW_POLICY_CONFIG` from its environment so tests can point it at fixtures,
and Claude Code hands it the developer's environment. A developer who sets
them runs on their own file. Closing that (a build-time switch, or accepting
only root-owned files) is tracked as follow-up work; until then the
guarantee is against accident, not intent. Repo-scoped rules key on
`.git/config`'s origin, which is also the developer's to edit.

Install `aw-sync` and `aw-policy` once; `aw-sync` writes the drop-in that
names the helper, and every change after that is a server-side decision.

**A helper that exits non-zero, or emits settings that fail schema validation,
makes Claude Code refuse to start.** The helper must therefore always exit 0.
That contract is the highest-severity path in the project.

`aw-policy` is that helper, and it is offline. `aw-sync`, running as root on
a timer, fetches the enrolled user's bundle from the control plane and writes
it to Claude Code's system directory as `aw-bundle.json`, beside a drop-in
that names the helper. On every launch `aw-policy` reads that bundle, makes
the one decision left in it (which repository the session is in), validates
the result against the published Claude Code settings schema, and prints it.
With no bundle, or one it cannot use, it prints an envelope with no settings,
which leaves the static managed-settings files in force, and still exits 0.
It exits non-zero only when the organization sets `requireBundle`. Every run
appends one line to a local audit log. `deploy/aw-sync/README.md` installs
the timer; `deploy/managed-settings/README.md` describes the files it writes.

Server-managed settings from the claude.ai console shadow the helper
entirely, so an organization uses one channel or the other; `aw doctor`
detects the collision, and reports the bundle and aw-sync's last cycle.

## Layout

    cmd/aw/           wrapper CLI: run an agent, doctor, agents
    cmd/aw-policy/    the policyHelper executable, offline
    cmd/aw-sync/      root-side sync: enroll, render every agent's files
    cmd/awd/          control plane: serve, apply
    deploy/           managed-settings install templates
    internal/agent/   Adapter interface, registry, Prepare/Launch/Exec
    internal/agent/claude/schema/  vendored settings schema and validation
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

## Try it

With Docker, the whole control plane comes up on Postgres:

    docker compose -f deploy/docker-compose.yml up -d --build

It listens on :8080 with `AWD_ADMIN_TOKEN=change-me` unless you export
another. Without Docker, run it from source:

Run the control plane and apply a policy:

    go build -o awd ./cmd/awd
    AWD_ADDR=127.0.0.1:8080 ./awd serve &
    ./awd apply examples/org-policy.yaml --url http://127.0.0.1:8080

Apply needs an administrator token; set `AWD_ADMIN_TOKEN` for both the
server and the CLI. Then enroll a machine and fetch its bundle:

    export AWD_ADMIN_TOKEN=change-me
    TOKEN=$(./awd enroll-token alice@acme.com --url http://127.0.0.1:8080)
    curl -s -X POST http://127.0.0.1:8080/v1/machines/enroll \
      -d "{\"token\":\"$TOKEN\",\"name\":\"$(hostname)\",\"os\":\"linux\"}"
    # {"machineId":"...","credential":"...","user":"alice@acme.com"}
    curl -s -H 'Authorization: Bearer <credential>' http://127.0.0.1:8080/v1/bundle

The bundle is every rule that could apply to that user, with repository
matchers still in it; the machine resolves those per session. `aw-sync` does
the enrolling and the rendering on a real machine; see
`deploy/aw-sync/README.md` for installing it.

Fetch what a given developer would get:

    curl 'http://127.0.0.1:8080/v1/policy?group=platform&repo=github.com/acme/payments-api'

Run the sync and the policy helper the way a managed machine would, with the
files kept under the working directory so no root is needed:

    go build -o aw-sync ./cmd/aw-sync && go build -o aw-policy ./cmd/aw-policy
    TOKEN=$(./awd enroll-token alice@acme.com --url http://127.0.0.1:8080)
    AW_SYNC_TOKEN=$TOKEN ./aw-sync enroll --server http://127.0.0.1:8080 --agents claude --state-dir ./state
    ./aw-sync once --state-dir ./state --root claude=./claude-root
    AW_POLICY_BUNDLE=$PWD/claude-root/aw-bundle.json ./aw-policy

Stop `awd` and run `aw-policy` again: it needs no server. Run `aw-sync once`
again: it exits 1 and leaves the rendered files as they were.

Inspect what the wrapper would run, without running it:

    go build -o aw ./cmd/aw
    ./aw --policy <(curl -s http://127.0.0.1:8080/v1/policy) doctor

`doctor` prints the resolved binary, the exact arguments, the injected
environment and a note for every decision, plus aw-sync's last cycle
(`AW_SYNC_STATE_DIR=./state` points it at the directory above) and whether
a bundle is in place. It never executes the agent.

## Tests

    go test ./...

No test runs a real agent: fakes are executable files placed on a temporary
PATH. No test needs a network.

The Postgres store runs the same conformance suite as the in-memory one. It
needs a throwaway database and skips without one:

    AWD_TEST_DATABASE_URL=postgres://... go test ./internal/store/

## Security posture

This is a client-side control, not a security boundary. A developer with local
administrator rights can edit the managed source or run a modified client. Pair
it with scheduled MDM redeployment and network-level egress restriction. See
`docs/threat-model.md` when it lands.
