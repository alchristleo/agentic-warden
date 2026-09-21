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

Deploy the helper path once through MDM or a `managed-settings.json`; every
change after that is a server-side decision.

**A helper that exits non-zero, or emits settings that fail schema validation,
makes Claude Code refuse to start.** The helper must therefore always exit 0,
falling back to its cached policy. That contract is the highest-severity path
in the project.

`aw-policy` is that helper. On every launch it asks the control plane for the
policy compiled for this user's groups and this repository, validates the
result against the published Claude Code settings schema, caches it, and
prints it. When the control plane is unreachable it prints the cached policy;
when there is no cache it prints an envelope with no settings, which leaves
the static `managed-settings.json` in force. It exits non-zero only when the
organization sets `requireFresh`. Every run appends one line to a local audit
log. `deploy/managed-settings/` has the install templates.

Server-managed settings from the claude.ai console shadow the helper
entirely, so an organization uses one channel or the other; `aw doctor`
detects the collision.

## Layout

    cmd/aw/           wrapper CLI: run an agent, doctor, agents
    cmd/aw-policy/    the policyHelper executable
    cmd/awd/          control plane: serve, apply
    deploy/           managed-settings install templates
    internal/agent/   Adapter interface, registry, Prepare/Launch/Exec
    internal/agent/claude/schema/  vendored settings schema and validation
    internal/policyhelper/  what aw-policy does: fetch, validate, cache, emit
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
matchers still in it; the machine resolves those per session. `aw-sync`,
which does the enrolling and the rendering on a real machine, is the next
milestone.

Fetch what a given developer would get:

    curl 'http://127.0.0.1:8080/v1/policy?group=platform&repo=github.com/acme/payments-api'

Run the policy helper the way Claude Code would, against that server:

    go build -o aw-policy ./cmd/aw-policy
    echo '{"serverUrl":"http://127.0.0.1:8080","groups":["platform"]}' > aw-policy.json
    AW_POLICY_CONFIG=$PWD/aw-policy.json ./aw-policy

Stop `awd` and run it again: the same policy comes back from the cache.

Inspect what the wrapper would run, without running it:

    go build -o aw ./cmd/aw
    ./aw --policy <(curl -s http://127.0.0.1:8080/v1/policy) doctor

`doctor` prints the resolved binary, the exact arguments, the injected
environment and a note for every decision. It never executes the agent.

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
