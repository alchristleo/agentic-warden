# Hosted cells runbook

How we run one `awd` control plane per design partner on AWS. Infrastructure is in
`deploy/aws/`: `platform/` (shared network, load balancer, certificate, image repository) and
`cell/` (one tenant, one state file each). Tenant variable files live in a private ops
repository, never in this repository.

Placeholders used below. Replace them everywhere:

| Placeholder | Meaning |
|---|---|
| `<region>` | AWS region of the platform, for example `eu-west-1` |
| `<state-bucket>` | S3 bucket for OpenTofu state, globally unique |
| `<domain>` | Base domain. Its Route 53 hosted zone must already exist in this account |
| `<tenant>` | Tenant name, `^[a-z][a-z0-9-]{1,30}$`. The cell is served at `https://<tenant>.<domain>` |
| `<account>` | 12-digit AWS account id |
| `<tag>` | Unique image tag, for example the git commit. The repository is immutable |
| `<hex>` | Image digest hex from step 3 |
| `<ops>` | Path to the private ops repository checkout |

Prerequisites: OpenTofu 1.13 (`tofu version`), AWS CLI v2 signed in to the account, Docker, Go.
Run every `tofu` command from the repository root.

## 1. Bootstrap the state bucket (once)

```sh
aws s3api create-bucket --bucket <state-bucket> --region <region> \
  --create-bucket-configuration LocationConstraint=<region>
aws s3api put-bucket-versioning --bucket <state-bucket> \
  --versioning-configuration Status=Enabled
aws s3api put-bucket-encryption --bucket <state-bucket> \
  --server-side-encryption-configuration '{"Rules":[{"ApplyServerSideEncryptionByDefault":{"SSEAlgorithm":"aws:kms"}}]}'
aws s3api put-public-access-block --bucket <state-bucket> \
  --public-access-block-configuration BlockPublicAcls=true,IgnorePublicAcls=true,BlockPublicPolicy=true,RestrictPublicBuckets=true
```

For `us-east-1`, drop the `--create-bucket-configuration` line. The state holds the generated
admin token, SCIM token, database password and console client secret in plain text, so this
bucket stays private, encrypted and versioned, and only operators get access to it.

## 2. Apply the platform (once per region)

The platform state key must be `platform.tfstate`: the cell root reads it from exactly that key.

```sh
tofu -chdir=deploy/aws/platform init \
  -backend-config=bucket=<state-bucket> \
  -backend-config=key=platform.tfstate \
  -backend-config=region=<region>
tofu -chdir=deploy/aws/platform apply -var region=<region> -var domain=<domain>
```

The apply waits for the wildcard certificate (`*.<domain>`) to validate through DNS records it
creates in your Route 53 zone; this takes a few minutes. Read back the repository URL:

```sh
tofu -chdir=deploy/aws/platform output -raw ecr_repository_url
```

It looks like `<account>.dkr.ecr.<region>.amazonaws.com/aw/awd`.

## 3. Push an image

Cells run x86_64 Fargate tasks, so build for `linux/amd64`. Tags are immutable; use a new `<tag>` each time.

```sh
ECR=<account>.dkr.ecr.<region>.amazonaws.com
aws ecr get-login-password --region <region> | docker login --username AWS --password-stdin $ECR
docker build --platform linux/amd64 -f deploy/Dockerfile -t $ECR/aw/awd:<tag> .
docker push $ECR/aw/awd:<tag>
aws ecr describe-images --region <region> --repository-name aw/awd \
  --image-ids imageTag=<tag> --query 'imageDetails[0].imageDigest' --output text
```

The last command prints `sha256:<hex>`. The cell image is `$ECR/aw/awd@sha256:<hex>`. The
module rejects anything without `@sha256:`.

## 4. Create a cell

In `<ops>/<tenant>.tfvars` (start from `deploy/aws/cell/example.tfvars`):

```hcl
tenant       = "<tenant>"
region       = "<region>"
state_bucket = "<state-bucket>"
image        = "<account>.dkr.ecr.<region>.amazonaws.com/aw/awd@sha256:<hex>"
```

Then:

```sh
tofu -chdir=deploy/aws/cell init -reconfigure \
  -backend-config=bucket=<state-bucket> \
  -backend-config=key=cells/<tenant>.tfstate \
  -backend-config=region=<region>
tofu -chdir=deploy/aws/cell plan  -var-file=<ops>/<tenant>.tfvars
tofu -chdir=deploy/aws/cell apply -var-file=<ops>/<tenant>.tfvars
```

`-reconfigure` matters: it points the working directory at this tenant's state. Run it every time
you switch tenant, before any plan or apply. On a first apply, `tofu -chdir=deploy/aws/cell state list`
must print nothing; if it lists resources, you are on another tenant's state.

The RDS instance takes about ten minutes to create. Outputs: `url`, `kms_key_arn`, `secret_arns`.

## 5. Hand over credentials

The admin token, SCIM token and database URL are Secrets Manager secrets named
`aw-<tenant>/admin-token`, `aw-<tenant>/scim-token` and `aw-<tenant>/database-url`.
Give the partner the admin token and, if they use SCIM, the SCIM token. Never send the database URL.

```sh
aws secretsmanager get-secret-value --region <region> \
  --secret-id aw-<tenant>/admin-token --query SecretString --output text
aws secretsmanager get-secret-value --region <region> \
  --secret-id aw-<tenant>/scim-token --query SecretString --output text
```

Send them out of band: a password manager share or an end-to-end encrypted message, one value per
message, never in the same channel as the URL. Tell the partner the SCIM base URL is
`https://<tenant>.<domain>/scim/v2`. To rotate the admin token, replace the generated value and
restart the service:

```sh
tofu -chdir=deploy/aws/cell apply -var-file=<ops>/<tenant>.tfvars \
  -replace=module.cell.random_password.admin_token
aws ecs update-service --region <region> --cluster aw --service aw-<tenant> --force-new-deployment
```

Use `module.cell.random_password.scim_token` for the SCIM token. Check the resource address with
`tofu -chdir=deploy/aws/cell state list | grep random_password` first.

## 6. Configure the console (optional)

The partner registers `awd` as a confidential OIDC client with their identity provider:

- Sign-in redirect URI: `https://<tenant>.<domain>/console/auth/callback`
- Scopes: `openid email profile`
- Entra ID: use `user_claim = "preferred_username"`.

They send us the issuer URL, client id and client secret, and tell us which IdP group holds their
console admins. Add to `<ops>/<tenant>.tfvars`:

```hcl
console = {
  issuer        = "https://<issuer>"
  client_id     = "<client-id>"
  client_secret = "<client-secret>"
  admin_group   = "<group>"
  user_claim    = "email"
}
```

Apply as in step 4. The secret is stored as `aw-<tenant>/console-client-secret` and reaches the
task as `AWD_CONSOLE_CLIENT_SECRET`. The group must come from the IdP (SCIM or `awd groups apply`);
a group that exists only in policy never grants console access. The admin token keeps working
as the recovery path.

## 7. Upgrade

Build and push a new image (step 3), put the new digest in `<ops>/<tenant>.tfvars`, then:

```sh
tofu -chdir=deploy/aws/cell init -reconfigure \
  -backend-config=bucket=<state-bucket> \
  -backend-config=key=cells/<tenant>.tfstate \
  -backend-config=region=<region>
tofu -chdir=deploy/aws/cell apply -var-file=<ops>/<tenant>.tfvars
```

The apply registers a new task definition and ECS rolls the service. Migrations run at start
under a database lock, so the old and new task can overlap safely. Watch it settle:

```sh
aws ecs describe-services --region <region> --cluster aw --services aw-<tenant> \
  --query 'services[0].deployments[].[status,runningCount,desiredCount]' --output text
curl -fsS https://<tenant>.<domain>/readyz
```

To roll back, put the previous digest back and apply. Schema migrations only go forward, so
check the release notes before rolling back across one.

## 8. Rotate the signing key

**The infrastructure does not support a zero-touch KMS rotation today.** `awd` itself does:
`AWD_SIGNING_KEY_PREVIOUS` names the key being rotated out, `awd` signs a rollover statement with it,
and enrolled machines repin to the new key without re-enrolling (procedure in "Rotating the signing
key" in `deploy/aw-sync/README.md`). But the cell module has no input for the previous key, and the
task role may call `kms:Sign` only on the cell's one key, so `awd` could not sign the rollover with
the old key even if the variable were set.

Option A, rotation without re-enrolling machines. It needs a module change that is not written yet:
a second key, both keys on the task role, `AWD_SIGNING_KEY` set to the new ARN and
`AWD_SIGNING_KEY_PREVIOUS` to the old one. Then: apply, watch `awd machines` until every KEY column
shows the new key id, drop the previous key, apply again.

Option B, available now, every machine re-enrolls:

```sh
tofu -chdir=deploy/aws/cell apply -var-file=<ops>/<tenant>.tfvars \
  -replace=module.cell.aws_kms_key.signing
```

This creates a new key, repoints the alias and the task, and schedules the old key for deletion in 7
days (`aws kms cancel-key-deletion --key-id <old-arn>` cancels it inside the window). Machines
pinned to the old key then fail verification. Each must re-enroll: with `AWD_ADMIN_TOKEN` set to
the tenant's admin token, mint a token with
`awd enroll-token <email> --url https://<tenant>.<domain>` and run
`aw-sync enroll --server https://<tenant>.<domain> --force` with `AW_SYNC_TOKEN` set to it.
Agree the date with the partner before you do this.

If the key itself may be compromised, use Option B at once and give the partner the CloudTrail
`Sign` events for the old key (see `trust.md`). Option B has not been exercised on a real account.

## 9. Backup and restore

RDS keeps automated backups for 7 days with point-in-time restore. Restore into a new instance and
swap it in under the same identifier, so OpenTofu stays consistent. This has not been rehearsed
against a real account; run it once on a throwaway cell before a partner needs it.

```sh
# 1. Read the network settings of the current instance.
aws rds describe-db-instances --region <region> --db-instance-identifier aw-<tenant> \
  --query 'DBInstances[0].[DBSubnetGroup.DBSubnetGroupName,VpcSecurityGroups[0].VpcSecurityGroupId]' --output text

# 2. Stop the service so nothing writes to the old database.
aws ecs update-service --region <region> --cluster aw --service aw-<tenant> --desired-count 0

# 3. Turn off deletion protection on the old instance and rename it out of the way.
aws rds modify-db-instance --region <region> --db-instance-identifier aw-<tenant> \
  --no-deletion-protection --apply-immediately
aws rds modify-db-instance --region <region> --db-instance-identifier aw-<tenant> \
  --new-db-instance-identifier aw-<tenant>-old --apply-immediately
aws rds wait db-instance-available --region <region> --db-instance-identifier aw-<tenant>-old

# 4. Restore to the chosen time under the original identifier.
aws rds restore-db-instance-to-point-in-time --region <region> \
  --source-db-instance-identifier aw-<tenant>-old \
  --target-db-instance-identifier aw-<tenant> \
  --restore-time <YYYY-MM-DDTHH:MM:SSZ> \
  --db-subnet-group-name <subnet-group-from-step-1> \
  --vpc-security-group-ids <sg-from-step-1> \
  --no-publicly-accessible --db-instance-class db.t4g.micro
aws rds wait db-instance-available --region <region> --db-instance-identifier aw-<tenant>

# 5. Let OpenTofu rewrite the database URL secret (new endpoint) and restore protection and retention.
tofu -chdir=deploy/aws/cell apply -var-file=<ops>/<tenant>.tfvars

# 6. Start the service again and check.
aws ecs update-service --region <region> --cluster aw --service aw-<tenant> --desired-count 1 --force-new-deployment
curl -fsS https://<tenant>.<domain>/readyz

# 7. Once the partner confirms the data, delete the old instance.
aws rds delete-db-instance --region <region> --db-instance-identifier aw-<tenant>-old \
  --skip-final-snapshot --delete-automated-backups
```

The restored instance keeps the old master password (the generated one in state), so no credential
changes. The step 5 apply should show the database URL secret, deletion protection and backup
retention changing; if it shows anything else, stop and read it.

## 10. Delete a cell

On the partner's written request, or at the end of the pilot. This destroys their data.

```sh
aws rds modify-db-instance --region <region> --db-instance-identifier aw-<tenant> \
  --no-deletion-protection --apply-immediately

tofu -chdir=deploy/aws/cell init -reconfigure \
  -backend-config=bucket=<state-bucket> \
  -backend-config=key=cells/<tenant>.tfstate \
  -backend-config=region=<region>
tofu -chdir=deploy/aws/cell destroy -var-file=<ops>/<tenant>.tfvars
```

The first command is needed because the module keeps deletion protection on. What is and is not removed:

- The RDS instance and its automated backups are deleted with it. No final snapshot is taken.
- The KMS key enters a 7-day pending-deletion window; after that it is gone for good.
  `aws kms cancel-key-deletion --key-id <arn>` works inside the window.
- The secrets have a 7-day recovery window, during which the same tenant name cannot be created
  again. To reuse the name now, run for each of `admin-token`, `scim-token`, `database-url` and
  `console-client-secret`:
  `aws secretsmanager delete-secret --region <region> --secret-id aw-<tenant>/<name> --force-delete-without-recovery`
- The CloudWatch log group `/aw/<tenant>` is deleted with the cell.
- The state file stays, and it holds the old secrets. List its versions and delete each one:

```sh
aws s3api list-object-versions --bucket <state-bucket> --prefix cells/<tenant>.tfstate \
  --query 'Versions[].[Key,VersionId]' --output text
aws s3api delete-object --bucket <state-bucket> --key cells/<tenant>.tfstate --version-id <version-id>
```

Repeat the second command for every version listed. Remove the tenant's tfvars from the ops repository.

## 11. First-apply verification

Run once, after the first real cell exists. This is the acceptance check for the whole chain.

1. The platform and cell applies finished without errors, and
   `tofu -chdir=deploy/aws/cell output -raw url` prints `https://<tenant>.<domain>`.
2. Health must print `200`:
   ```sh
   curl -sS -o /dev/null -w '%{http_code}\n' https://<tenant>.<domain>/readyz
   ```
   If it does not: `aws logs tail /aw/<tenant> --region <region> --since 15m`.
3. Plain http redirects (expect `301`):
   `curl -sSI http://<tenant>.<domain>/ | head -1`
4. Build the binaries, enroll a machine and sync:
   ```sh
   go build -o awd ./cmd/awd && go build -o aw ./cmd/aw && go build -o aw-sync ./cmd/aw-sync
   export AWD_ADMIN_TOKEN=$(aws secretsmanager get-secret-value --region <region> \
     --secret-id aw-<tenant>/admin-token --query SecretString --output text)
   TOKEN=$(./awd enroll-token you@example.com --url https://<tenant>.<domain>)
   AW_SYNC_TOKEN=$TOKEN ./aw-sync enroll --server https://<tenant>.<domain> --agents claude --state-dir ./state
   ./aw-sync once --state-dir ./state --root claude=./claude-root
   ```
5. Signature check, as in the README ("Inspect what the wrapper would run"):
   ```sh
   AW_SYNC_STATE_DIR=./state ./aw --policy <(curl -s https://<tenant>.<domain>/v1/policy) doctor
   ```
   The output must include `bundle signature: verified (v2, key ...)`.
6. Signing is logged. This shows `Sign` events from the cell's task role (CloudTrail can lag a few minutes):
   ```sh
   aws cloudtrail lookup-events --region <region> \
     --lookup-attributes AttributeKey=EventName,AttributeValue=Sign --max-results 5
   ```
7. If the console is configured, open `https://<tenant>.<domain>/console/` and sign in as a member
   of the admin group.

Record the result (date, region, image digest) against acceptance criterion 11 in
`docs/superpowers/specs/2026-10-23-hosted-cells-design.md`.
