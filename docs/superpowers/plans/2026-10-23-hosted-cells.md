# Hosted cells — plan

Spec: `docs/superpowers/specs/2026-10-23-hosted-cells-design.md`. Base: `7ba02b2` on branch `hosted-cells`.

## Global constraints

- Go: `GOTOOLCHAIN=auto` for normal runs; before reporting done also run `GOTOOLCHAIN=go1.27.3 go test ./...` (CI uses Go stable). Never plain `go mod tidy`.
- OpenTofu 1.13.2 is installed at `~/.local/bin/tofu`. Provider `hashicorp/aws ~> 6.70`, `hashicorp/random ~> 3.6`. `tofu test` uses `mock_provider "aws"` (no credentials, no network to AWS). Commit `.terraform.lock.hcl` for each root; never commit `.terraform/`, state, or tfvars with real tenant data (add `.gitignore` entries under `deploy/aws/`).
- Postgres suite: `docker run -d --name awd-pg17 -e POSTGRES_PASSWORD=pw -p 5433:5432 postgres:17`, `AWD_TEST_DATABASE_URL='postgres://postgres:pw@localhost:5433/postgres?sslmode=disable'`. Stop and remove your container when done.
- Security rows (each must hold and, where marked, be asserted by a test):
  - RDS private, encrypted, deletion protection on, 7-day backups (test).
  - KMS key `ECC_NIST_EDWARDS25519` / `SIGN_VERIFY`; task role allows only `kms:Sign` + `kms:GetPublicKey` on this cell's key ARN (test).
  - Execution role reads only this cell's secrets (test).
  - Task SG ingress only from the ALB SG on the container port (test).
  - Admin token, SCIM token, DB URL only in `secrets`, never `environment` (test).
  - Image must be a digest (`@sha256:`) (variable validation + test).
  - ALB: 443 with `ELBSecurityPolicy-TLS13-1-2-2021-06`, 80 redirects to 443, SG inbound only 80/443 (test).
  - No tenant names or secrets in the repo; examples use `example` tenant and `example.com`.

## Platform outputs contract (T3 produces, T4 consumes via `terraform_remote_state`)

`vpc_id`, `public_subnet_ids` (list), `alb_security_group_id`, `https_listener_arn`, `alb_dns_name`,
`alb_zone_id`, `ecs_cluster_arn`, `ecr_repository_url`, `zone_id`, `domain`.

## Parallel group A (no shared files) — dispatch together, each in a worktree

### T1 — migration advisory lock (agent: sdd-implementer)
Files: `internal/store/postgres.go`, `internal/store/postgres_test.go` (or the suite file that holds Postgres-only tests).
- `migrate()` holds `pg_advisory_lock(<fixed int64 constant>)` on one acquired connection for the whole run, releases it after (also on error).
- Test `TestConcurrentMigrate` (Postgres-only, skipped without `AWD_TEST_DATABASE_URL`): fresh database (create a uniquely named database, drop it at cleanup), 4 goroutines open the store concurrently, all succeed. Show it red before the lock (run it a few times if the race is flaky; report the red evidence).
- Then run the whole store suite on postgres:17 (AC5).

### T2 — `AWD_CONSOLE_CLIENT_SECRET` (agent: sdd-implementer)
Files: `internal/config/config.go`, `internal/config/config_test.go`, `cmd/awd/main.go` (+ its test), README env table if one lists `_FILE`.
- Either env var supplies the console OIDC client secret; both set → startup error naming both; empty value → error as for the file today. Help text lists the new variable.
Tests: `TestConsoleClientSecretFromEnv`, `TestConsoleClientSecretBothSetIsAnError`, existing `_FILE` tests still pass.

### T3 — platform root (agent: sdd-implementer)
Files: `deploy/aws/platform/{versions,main,variables,outputs}.tf`, `deploy/aws/platform/tests/platform.tftest.hcl`, `deploy/aws/platform/.terraform.lock.hcl`, `deploy/aws/.gitignore`.
- Variables: `region`, `domain` (zone must already exist in Route 53: `data "aws_route53_zone"`), `name` (default `aw`).
- VPC with 2 public subnets in 2 AZs + IGW; ALB + SG; wildcard ACM cert `*.<domain>` with DNS validation records; 443 listener (default action fixed 404), 80→443 redirect; ECR repo (`image_tag_mutability = IMMUTABLE`, scan on push); ECS cluster. Backend `s3` partial config (`use_lockfile = true`).
- Outputs exactly the contract above.
Tests (mock provider, `command = plan`): listener policy + cert; 80 redirect; ALB SG inbound only 80/443; outputs present.
Checks: AC1 (platform), AC3.

### T4 — cell module + cell root (agent: sdd-implementer)
Files: `deploy/aws/cell/{versions,main,variables,outputs}.tf` (incl. `tests/cell.tftest.hcl`; the former `modules/cell` is collapsed into the root), `deploy/aws/cell/.terraform.lock.hcl`, `deploy/aws/cell/example.tfvars` (placeholder tenant `example`).
- Module inputs: `tenant` (validation `^[a-z][a-z0-9-]{1,30}$`), `image` (validation: contains `@sha256:`), the platform contract values, optional `console = { issuer, client_id, client_secret, admin_group, user_claim }` (null = no console).
- Resources per spec Design: KMS key (+ alias `alias/aw-<tenant>`, deletion window 7), `random_password` ×3 → Secrets Manager (admin token, SCIM token, DB URL composed with the RDS endpoint, `sslmode=require`; console secret when set), DB subnet group + RDS `db.t4g.micro` postgres 17 (private, encrypted, deletion protection, backup 7, `delete_automated_backups = true`, `skip_final_snapshot = true`), RDS SG ingress only from task SG on 5432, task SG ingress only from ALB SG, IAM task role (KMS only) + execution role (logs + only this cell's secrets), log group 30 days, task definition (port 8080, health `/readyz` on target group), service `desired_count = 1` public IP on public subnets, target group + listener rule host `<tenant>.<domain>`, Route 53 alias record.
- Env: `AWD_ADDR=:8080`, `AWD_PUBLIC_URL`, `AWD_SIGNING_KEY=awskms:<arn>`, `AWD_LOG_LEVEL=info`, console non-secret vars; secrets: `AWD_DATABASE_URL`, `AWD_ADMIN_TOKEN`, `AWD_SCIM_TOKEN`, `AWD_CONSOLE_CLIENT_SECRET` (when console set; T2 adds this env var).
- Root `cell/`: backend s3 partial, `terraform_remote_state` for platform, calls module; outputs `url`, `kms_key_arn`, secret ARNs (not values).
Tests (mock provider, plan): every security row above for the cell, including a second `run` with a bad image (expect_failures on the variable) and a bad tenant name.
Checks: AC1 (cell), AC2.

## Group B (after A merges)

### T5 — infra CI (agent: sdd-implementer-lite)
Files: `.github/workflows/infra.yml`. On push/PR paths `deploy/aws/**` and the workflow itself; `permissions: contents: read`; checkout with `persist-credentials: false`; `opentofu/setup-opentofu` pinned to OpenTofu 1.13.2; steps: `tofu fmt -check -recursive deploy/aws`, then for platform and cell `init -backend=false` + `validate`, then `tofu test` in `platform` and `cell`. Check: AC7 (grep) and the commands pass locally.

### T6 — runbook and trust statement (agent: sdd-implementer)
Files: `docs/hosting/runbook.md`, `docs/hosting/trust.md`, one link from README.
- Runbook sections with exact commands (AC8): bootstrap state bucket (encrypted, versioned, public access blocked), platform apply, push image (build, tag, push to ECR, read the digest), create cell (`tofu init -backend-config=…key=cells/<tenant>.tfstate`, `-var-file` from the private ops repo), hand over credentials (read admin/SCIM tokens from Secrets Manager, send out of band), configure console (tenant IdP redirect URI `https://<tenant>.<domain>/console/auth/callback`), upgrade (new digest, apply), rotate signing key (new KMS key, `AWD_SIGNING_KEY_PREVIOUS` window per the signature-v2 spec), backup and restore (RDS PITR to new instance, point DB URL secret), delete cell (disable deletion protection, destroy, KMS 7-day window), first-apply verification (AC11 steps).
- Trust statement (AC9), honest pilot terms.
Use the variable and output names from T3/T4 exactly.

## Ledger

```
T2 done 02c2d62 sdd-implementer AWD_CONSOLE_CLIENT_SECRET; review approve w/ 1 should
T1 done 6d8948c sdd-implementer advisory lock; first hand-back was "placeholder", re-asked; review approve (independent pg17 + mutation)
T2 fix 0a893bc sdd-implementer extracted consoleClientSecret + table test
T3 done 8e261fc sdd-implementer platform root; review approve (3 mutations caught)
T4 done 8411504 sdd-implementer cell module+root; agent stuck on background tofu init, stopped; review fix-needed (DB SG test vacuous)
T6 done 1bfa93b sdd-implementer runbook+trust; gaps: no rotation, invented 5-day SLA (user chose 30 days dd831c8); review deferred to panel (user OK)
T4 fix 5bda8b9 sdd-implementer DB SG source asserted (apply run); re-review approve
T5 done 98628cb sdd-implementer-lite infra.yml; 160k tokens fighting stalled provider downloads; review approve
PANEL correctness fix-needed(2 must) design fix-needed(2 must) security fix-needed(1 must) /security-review 0 findings; must: ECR ARN, AWD_PUBLIC_URL w/o console, KMS key policy, restore section; user picks: enforce key policy, 90-day CloudTrail statement, fix+mark restore unrehearsed, collapse module into root
```

## Backlog

- Co-signed revisions with a tenant-held key.
- CI image release with GitHub OIDC → AWS (ECR push), image signing.
- Alarms (target unhealthy, 5xx) and a PITR restore drill.
- `sslmode=verify-full` with the RDS CA bundle.
- CI Go matrix `[go.mod floor, stable]` (from the platform-refresh retro).
- `deploy/aws/cell.sh <tenant> plan|apply|destroy|output` wrapper (panel design; user deferred).
- Delete AWD_ADDR/AWD_LOG_LEVEL defaults, KMS alias, platform `name` knob → local, collapse .gitignore files (panel design; user deferred).
- Trim change-detector asserts in tofu tests (panel design; user deferred).
- CloudTrail trail with 1-year retention (user chose the 90-day statement for the pilot).
- Pin actions to commit SHAs (panel security nit).
- Local provider mirror script for this WSL box: registry downloads stall (T5 spent ~160k tokens).

## Retro (2026-10-24, merged as PR #2 `87fe1f0`)

- **Numbers:** 6 tasks; fix rounds T2 1, T4 1, panel wave (infra + docs) 1, +1 key-policy test round (user-approved); 0 blocked; panel must-fix 4; `/security-review` 0 findings; 5h usage ~1% → 65%.
- **Wasted effort → structure (applied, see lean-sdd CHANGELOG 2026-10-24):** hung background `tofu init` in 3 agents → implementers stop background processes before reporting; 2 vacuous security tests passed TDD → implementers mutation-check every security assertion; 3 false Haiku-trailer findings → lite reviewer checks `.git/claude-trailer`; ECR ARN bug hidden by a fake fixture → consumer fixtures use producer's real-shaped values (lean-sdd step 4).
- **Still open:** local provider mirror for this WSL box (Backlog); AC11 first real apply is the user's.
