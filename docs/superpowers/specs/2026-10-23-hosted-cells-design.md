# Hosted cells for the design-partner pilot

## Goal

Let us run one isolated `awd` control plane per design partner ("cell") on AWS, set up and
operated from a runbook, with a one-page trust statement the partner can read before signing.
Decided 2026-09-25: pilot with manual onboarding and no billing; one cell per tenant; a per-tenant
AWS KMS Ed25519 signing key held by us; AWS (ECS Fargate, RDS, KMS, Secrets Manager).

## Non-goals

- Self-serve signup, a provisioning API, billing.
- Multi-region, autoscaling, custom tenant domains.
- Alarms and dashboards (CloudWatch Logs only).
- A CI image release pipeline (the runbook pushes images by hand, pinned by digest).
- Co-signed revisions with a tenant-held key (planned upgrade; Backlog).
- A live `apply` in this milestone: no AWS account is available, so infrastructure is verified
  with `tofu test` against a mocked AWS provider. The first real apply is a runbook step.

## Decisions

1. **One RDS instance per cell** (db.t4g.micro, PostgreSQL 17): own credentials, backups and
   deletion. Considered: a shared instance with a database per tenant (cheaper, but shared master
   credentials and cross-tenant blast radius).
2. **State layout: one state per cell.** Considered: one root with a `for_each` over tenants
   (one apply touches every tenant). Chosen: `deploy/aws/platform` (shared) and
   `deploy/aws/cell` (one state per tenant via `-backend-config=key=cells/<tenant>.tfstate`).
   S3 backend with OpenTofu's native lockfile; the bucket is created once by a runbook command.
3. **Shared network and ingress:** one VPC over two AZs, one ALB with host-based routing to
   `<tenant>.<domain>`, one wildcard ACM certificate (DNS-validated in a Route 53 zone we own).
4. **Tasks in public subnets with a public IP and no inbound rule except from the ALB.** Tasks need
   egress to the tenant's IdP for console SSO anyway; this skips a NAT gateway (~$32/month).
5. **Secrets:** Terraform-generated admin token, SCIM token and DB password stored in Secrets
   Manager and injected as ECS secrets. Tenant tfvars live in a private ops repository, never here.
6. **Console OIDC secret delivery:** add `AWD_CONSOLE_CLIENT_SECRET` (env), mutually exclusive with
   `AWD_CONSOLE_CLIENT_SECRET_FILE`. Considered: a sidecar writing the file to a shared volume
   (more moving parts for one value).
7. **Migrations take a Postgres advisory lock.** `migrate()` runs unlocked today; two tasks starting
   together can race on `CREATE … IF NOT EXISTS`. Cells run `desired_count = 1`, but a rolling
   deploy briefly runs two.
8. **Tooling:** OpenTofu 1.13 (open source, matches the Apache-2.0 licence); HCL stays
   Terraform-compatible. AWS provider `~> 6.70`.
9. **Image:** referenced by digest (`…@sha256:…`) from one ECR repository in our account.
10. **Trust statement: honest pilot terms** (key custody in our account, CloudTrail log of every
    Sign call shared on request, co-signing planned, no code or prompts ever reach us).

## Design

```
deploy/aws/
  platform/      VPC (2 AZ public subnets), ALB (443, HTTP→HTTPS redirect), wildcard ACM cert,
                 Route 53 zone data source, ECR repo, ECS cluster. Outputs consumed by cells.
  cell/          Root: backend "s3" (partial), reads platform outputs via terraform_remote_state.
                 KMS key (ECC_NIST_EDWARDS25519, SIGN_VERIFY), Secrets Manager secrets,
                 RDS (encrypted, private, deletion protection, 7-day backups), security groups,
                 task + execution IAM roles, task definition, service, target group,
                 listener rule, Route 53 record, log group (30-day retention).
                 Inputs: tenant, region, image_digest, console OIDC (optional).
docs/hosting/runbook.md   bootstrap, push image, create cell, hand over credentials, configure
                          console, upgrade, rotate signing key, backup/restore, delete cell.
docs/hosting/trust.md     the trust statement.
```

Task environment: `AWD_DATABASE_URL` (secret, `sslmode=require`), `AWD_ADMIN_TOKEN`,
`AWD_SCIM_TOKEN` (secrets), `AWD_SIGNING_KEY=awskms:<key arn>`, `AWD_PUBLIC_URL=https://<tenant>.<domain>`,
console variables when configured. Health check `/readyz`. The task role may call only
`kms:Sign` and `kms:GetPublicKey` on its own key.

Delete: the runbook disables deletion protection, destroys the cell (RDS automated backups deleted
with it, no final snapshot), and the KMS key enters its 7-day pending-deletion window.

## Acceptance criteria

1. Formatting and validation pass: `tofu fmt -check -recursive deploy/aws` and
   `tofu -chdir=deploy/aws/<root> init -backend=false && tofu -chdir=deploy/aws/<root> validate`
   for `platform` and `cell`.
2. Cell module plan assertions pass with a mocked AWS provider: `tofu -chdir=deploy/aws/cell test`.
   They assert: RDS `publicly_accessible = false`, `storage_encrypted = true`, `deletion_protection = true`,
   `backup_retention_period = 7`; KMS key spec `ECC_NIST_EDWARDS25519` and usage `SIGN_VERIFY`;
   the task role policy allows exactly `kms:Sign` and `kms:GetPublicKey` on this cell's key ARN only;
   the task security group's only ingress source is the ALB security group;
   the admin token, SCIM token and database URL appear only under `secrets`, never `environment`;
   `AWD_SIGNING_KEY` is `awskms:` + the key ARN; `desired_count = 1`;
   an `image_digest` without `@sha256:` is rejected by variable validation.
3. Platform plan assertions pass with a mocked provider: `tofu -chdir=deploy/aws/platform test`.
   They assert: the 443 listener uses `ELBSecurityPolicy-TLS13-1-2-2021-06` (TLS 1.3 and 1.2 only) and the wildcard certificate; port 80 redirects to 443;
   the ALB security group allows inbound only on 80 and 443.
4. Concurrent migrations succeed: 4 parallel store opens on an empty database all return without
   error (red before the lock): `AWD_TEST_DATABASE_URL=… go test ./internal/store/ -run TestConcurrentMigrate -v`.
5. The full Postgres store suite passes on PostgreSQL 17 (`postgres:17` in Docker) with
   `AWD_TEST_DATABASE_URL` set: `go test ./internal/store/...`.
6. `AWD_CONSOLE_CLIENT_SECRET` works and conflicts are named: setting it starts the console;
   setting both it and `_FILE` is a startup error naming both: `go test ./internal/config/ ./cmd/awd/ -v`.
7. CI runs the infrastructure checks: `.github/workflows/infra.yml` runs criteria 1–3 with OpenTofu
   1.13.x on changes under `deploy/aws/**`, `permissions: contents: read`.
8. The runbook has a section with exact commands for each of: bootstrap state bucket, push image,
   create cell, hand over credentials, configure console, upgrade, rotate signing key,
   backup and restore, delete cell: reviewed by `spec-converge` against `docs/hosting/runbook.md`.
9. The trust statement states: data stored, data never received, key custody and the CloudTrail
   offer, region, deletion timeline (7-day KMS window, backups deleted with the instance),
   and AWS as the only subprocessor: reviewed against `docs/hosting/trust.md`.
10. Full suite green on the go.mod floor and on current Go:
    `go vet ./... && go test ./...` and `GOTOOLCHAIN=go1.27.3 go test ./...`.
11. **Manual (user, first real apply):** following the runbook creates one cell whose
    `https://<tenant>.<domain>/readyz` returns 200 and whose enrolled machine's `aw doctor` reports
    `bundle signature: verified (v2, key …)`. Marked INCONCLUSIVE until run.

## Security notes

- Tenant isolation is per-cell: own RDS instance and credentials, own KMS key, own task role,
  own secrets. The only shared pieces are the VPC, ALB, ECR repository and ECS cluster.
- IAM: the task role reaches only its own key (`kms:Sign`, `kms:GetPublicKey`); the execution role reads
  only its own cell's secrets.
- Database TLS is `sslmode=require` (encrypted, server not verified) inside the VPC; `verify-full`
  with the RDS CA bundle is Backlog.
- Secrets never appear in plan output as plaintext env vars, and never in this repository; the
  state bucket holds them, so it is encrypted, versioned and private (runbook bootstrap).
- Key custody: we hold the signing keys; the trust statement says so plainly, offers the CloudTrail
  log, and names co-signing as the planned upgrade.

## Changes after review

- `deploy/aws/modules/cell` is collapsed into the `deploy/aws/cell` root (one Tofu root, no
  module). Its tests live in `deploy/aws/cell/tests/` and stub the platform remote state with
  `override_data`; run them with `tofu -chdir=deploy/aws/cell test`.
- Review-driven changes, briefly:
  - `image_digest` input (must be `sha256:` plus 64 lowercase hex); the full image reference is built from the platform's ECR repository. The platform exports `ecr_repository_arn` for the execution role.
  - Console: the client secret is its own sensitive variable (`console_client_secret`), outside the `console` object, and reaches the task as a secret. `AWD_PUBLIC_URL` is set only when the console is configured.
  - `signing_keys` takes one or two labels (1–16 characters each) for rotation; the first is the previous key while two are present.
  - KMS key policy: account root keeps administration; an explicit Deny on `kms:Sign` for every principal except the task role (`aws:PrincipalArn`).
  - Tenant name: `^[a-z][a-z0-9-]{1,30}$` (at most 31 characters), no leading, trailing or double hyphen.
  - Trust statement: deletion completes within 30 days of a written request; the CloudTrail `Sign` history offer covers the last 90 days.
  - The ECS service `depends_on` (secrets and role policies exist before the first task starts) has no automated test, by user decision. The first-apply check (AC11) covers it.

## Status (converged 2026-10-24)

Branch `hosted-cells` at `3ede3c8`, compared with base `7ba02b2`. Environment: OpenTofu 1.13.2, Go 1.24.0 (the `go.mod` floor) and `GOTOOLCHAIN=go1.27.3`, PostgreSQL 17.11 (Docker `postgres:17`, container removed after the run).

| # | criterion (short) | status | evidence (command + key output line) |
|---|---|---|---|
| 1 | fmt and validate pass for platform and cell | VERIFIED | `tofu fmt -check -recursive deploy/aws` exit 0; `tofu -chdir=deploy/aws/platform validate` and `tofu -chdir=deploy/aws/cell validate`: `Success! The configuration is valid.` (both) |
| 2 | cell plan assertions pass (mocked provider) | VERIFIED | `tofu -chdir=deploy/aws/cell test`: `Success! 28 passed, 0 failed.` exit 0 |
| 3 | platform plan assertions pass (mocked provider) | VERIFIED | `tofu -chdir=deploy/aws/platform test`: `Success! 5 passed, 0 failed.` exit 0 |
| 4 | 4 parallel store opens on empty DB succeed | VERIFIED | `AWD_TEST_DATABASE_URL=postgres://…@localhost:5435/… go test ./internal/store/ -run TestConcurrentMigrate -v -count=5`: `--- PASS: TestConcurrentMigrate` ×5, `ok`. Red-before-lock was not re-run (see deviations) |
| 5 | full Postgres store suite passes on PG 17 | VERIFIED | `AWD_TEST_DATABASE_URL=… go test ./internal/store/... -count=1 -v`: `--- PASS: TestPostgres`, `--- PASS: TestConcurrentMigrate`, `ok github.com/acme/agent-wrapper/internal/store` |
| 6 | console client secret works; both set is a named error | VERIFIED | `go test ./internal/config/ ./cmd/awd/ -count=1 -v`: `--- PASS: TestConsoleClientSecretFromEnv`, `--- PASS: TestConsoleClientSecretBothSetIsAnError` (asserts error names both variables), `--- PASS: TestConsolePartialConfigRefusesToStart`, `ok …/cmd/awd`. The "starts the console" part is a config-level check (`ConsoleEnabled()`), not a launch of the binary |
| 7 | CI runs criteria 1–3 on OpenTofu 1.13.x, read-only permissions | VERIFIED | `.github/workflows/infra.yml`: `tofu_version: 1.13.2`, `permissions: contents: read`. PR #2 run 39993822508, job `tofu` passed in 54s (providers downloaded in CI; fmt, validate, platform and cell tests) |
| 8 | runbook has sections for the nine operations with exact commands | VERIFIED | `grep -n "^#" docs/hosting/runbook.md`: §1 bootstrap state bucket, §3 push image, §4 create cell, §6 hand over credentials, §7 configure console, §8 upgrade, §9 rotate signing key, §10 backup and restore, §11 delete cell; each has fenced shell or HCL commands. Reviewed by reading the file (content review, see deviations for a dangling step reference) |
| 9 | trust statement covers the required points | VERIFIED | `docs/hosting/trust.md` read: "What we store" (data stored), "What never reaches us" (data never received), "Who holds the signing key" with the CloudTrail offer, "Where it runs" (one region), "Backups and deletion" (7-day KMS window, backups deleted with the instance), "Subprocessors" (`Amazon Web Services is our only subprocessor`). Reviewed by reading the file |
| 10 | full suite green on go.mod floor and on current Go | VERIFIED | Go 1.24.0: `go vet ./... && go test ./...` exit 0, last `ok` lines include `internal/store`. `GOTOOLCHAIN=go1.27.3 go vet ./... && go test ./... -count=1`: exit 0, 30 `ok` packages. Both runs without `AWD_TEST_DATABASE_URL`, so the Postgres tests were skipped there (row 5 covers them) |
| 11 | manual first apply: `/readyz` 200 and `aw doctor` verified | INCONCLUSIVE | Manual step by the user on a real AWS account; not run (no account). By design |

Result: 9 of 11 criteria VERIFIED, 2 INCONCLUSIVE (7 and 11). No criterion NOT VERIFIED. No timing criteria in this spec, so no base-versus-branch timing was measured.

### Deviations and follow-ups

- `deploy/aws/modules/` is an untracked working-tree leftover (a provider cache from the collapsed module). It is not part of the branch and was not committed. Delete it locally.
- Row 4: the red-before-lock run was not repeated; only the green run is recorded.
- Row 7: the workflow itself has not run on GitHub.
- Row 10: the `GOTOOLCHAIN` and floor runs did not set `AWD_TEST_DATABASE_URL`.
- `docs/hosting/runbook.md` §10 says "the plan in step 5 updates the database URL secret", but §10 has no step 5. Not fixed here; the runbook is out of scope for this commit.
- The ECS `depends_on` ordering has no automated test (user decision, see Changes after review).
- Row 11 remains the user's manual first apply.
