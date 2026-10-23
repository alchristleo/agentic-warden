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
  modules/cell/  KMS key (ECC_NIST_EDWARDS25519, SIGN_VERIFY), Secrets Manager secrets,
                 RDS (encrypted, private, deletion protection, 7-day backups), security groups,
                 task + execution IAM roles, task definition, service, target group,
                 listener rule, Route 53 record, log group (30-day retention).
  cell/          Root: backend "s3" (partial), reads platform outputs via terraform_remote_state,
                 calls modules/cell. Inputs: tenant, region, image_digest, console OIDC (optional).
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
2. Cell module plan assertions pass with a mocked AWS provider: `tofu -chdir=deploy/aws/modules/cell test`.
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
