# agent-wrapper

Enterprise control plane for coding agents. An organization authors one policy;
each developer's session gets the slice of it that applies to them, targeted by
group and by repository.

Status: early. Claude Code is the only agent implemented.

## Why this exists

Claude Code already ships a lot of what an organization needs: managed settings
files, MDM delivery, server-managed settings from the claude.ai console,
`managedMcpServers`, sandboxing, and OpenTelemetry. What it does not offer is
**per-group targeting** (server-managed settings apply uniformly to the whole
organization), identity-bound short-lived credentials, or enforced per-team
budgets. This project fills those gaps, on an agent-agnostic interface so other
agents can follow.

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

## Layout

    cmd/aw/           wrapper CLI: run an agent, doctor, agents
    cmd/awd/          control plane: serve, apply
    internal/agent/   Adapter interface, registry, Prepare/Launch/Exec
    internal/policy/  rules, targeting, compilation, YAML/JSON authoring
    internal/merge/   settings deep-merge and environment merge
    internal/decide/  tool-call decisions; the seam for a semantic decider
    internal/store/   policy revisions (in-memory and Postgres)
    internal/handler/ the HTTP layer
    internal/glob     wildcard matching for rules and repo targeting
    internal/cache/   content-addressed generated files
    internal/config/  control plane configuration

## Try it

Run the control plane and apply a policy:

    go build -o awd ./cmd/awd
    AWD_ADDR=127.0.0.1:8080 ./awd serve &
    ./awd apply examples/org-policy.yaml --url http://127.0.0.1:8080

Fetch what a given developer would get:

    curl 'http://127.0.0.1:8080/v1/policy?group=platform&repo=github.com/acme/payments-api'

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
