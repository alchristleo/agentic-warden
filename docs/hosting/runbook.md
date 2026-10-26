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
| `<hex>` | Image digest hex (64 characters) from step 3 |
| `<ops>` | Absolute path to the private ops repository checkout. It must be absolute: a relative `-var-file` is resolved against `-chdir`, not your current directory |

Prerequisites: OpenTofu 1.13 (`tofu version`), AWS CLI v2 signed in to the account, Docker, Go.
The identity that applies a cell needs `ec2:DescribeVpcs`. The cell reads the platform VPC to set `AWD_TRUSTED_PROXIES`.
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
tofu -chdir=deploy/aws/platform apply -var-file=<ops>/platform.tfvars
```

`<ops>/platform.tfvars` holds the inputs:

```hcl
region = "<region>"
domain = "<domain>"
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

The last command prints `sha256:<hex>`. The cell takes only that digest as `image_digest`; it
builds the full image reference from the platform's repository itself, and rejects anything that
is not `sha256:` followed by 64 lowercase hex characters.

## 4. Create a cell

In `<ops>/<tenant>.tfvars` (start from `deploy/aws/cell/example.tfvars`):

```hcl
tenant       = "<tenant>"
region       = "<region>"
state_bucket = "<state-bucket>"
image_digest = "sha256:<hex>"
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

Run that same `init -reconfigure` before every cell command in this runbook (plan, apply, import,
destroy, state, output). It points the working directory at this tenant's state, so it matters most
when you switch tenant. On a first apply, `tofu -chdir=deploy/aws/cell state list` must
print nothing; if it lists resources, you are on another tenant's state.

If the cell has a console (step 7), also have `TF_VAR_console_client_secret` set for every plan,
apply, import and destroy.

The RDS instance takes about ten minutes to create. Outputs: `url`, `kms_key_arns` (a map of key
label to ARN) and `secret_arns`.

## 5. First-apply verification

Run after the first apply of a cell, and the first time on any new account.

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
   AW_SYNC_STATE_DIR=./state ./aw --policy <(curl -s -H "Authorization: Bearer $AWD_ADMIN_TOKEN" https://<tenant>.<domain>/v1/policy) doctor
   ```
   The output must include `bundle signature: verified (v2, key ...)`.
6. Signing is logged. `<key-arn>` is the cell's current key: the ARN of the last label in `signing_keys` (`k1` unless you have rotated) from `tofu -chdir=deploy/aws/cell output kms_key_arns`. This shows `Sign` events from the cell's task role (CloudTrail can lag a few minutes):
   ```sh
   aws cloudtrail lookup-events --region <region> \
     --lookup-attributes AttributeKey=ResourceName,AttributeValue=<key-arn> --max-results 5
   ```
7. If the console is configured, open `https://<tenant>.<domain>/console/` and sign in as a member
   of the admin group.

## 6. Hand over credentials

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
tofu -chdir=deploy/aws/cell init -reconfigure \
  -backend-config=bucket=<state-bucket> \
  -backend-config=key=cells/<tenant>.tfstate \
  -backend-config=region=<region>
tofu -chdir=deploy/aws/cell apply -var-file=<ops>/<tenant>.tfvars \
  -replace=random_password.admin_token
aws ecs update-service --region <region> --cluster aw --service aw-<tenant> --force-new-deployment
```

Use `random_password.scim_token` for the SCIM token. Check the resource address with
`tofu -chdir=deploy/aws/cell state list | grep random_password` first.

## 7. Configure the console (optional)

The partner registers `awd` as a confidential OIDC client with their identity provider:

- Sign-in redirect URI: `https://<tenant>.<domain>/console/auth/callback`
- Scopes: `openid email profile`
- Entra ID: use `user_claim = "preferred_username"`.

They send us the issuer URL, client id and client secret, and tell us which IdP group holds their
console admins. Add the non-sensitive settings to `<ops>/<tenant>.tfvars`:

```hcl
console = {
  issuer      = "https://<issuer>"
  client_id   = "<client-id>"
  admin_group = "<group>"
  user_claim  = "email"
}
```

The client secret never goes in a tfvars file or the ops repository. Read it from the password
manager into the environment without echoing it or leaving it in shell history, then apply as in
step 4:

```sh
read -rs TF_VAR_console_client_secret && export TF_VAR_console_client_secret
```

Paste the secret and press Enter. The variable is gone when you close the shell. The secret is
stored as `aw-<tenant>/console-client-secret` and reaches the task as `AWD_CONSOLE_CLIENT_SECRET`.
Setting `console` also sets `AWD_PUBLIC_URL`; it is not set without the console.

When only the secret changes, the task definition does not change, so the running task keeps the
old value. After the apply, restart it:

```sh
aws ecs update-service --region <region> --cluster aw --service aw-<tenant> --force-new-deployment
```

The group must come from the IdP (SCIM or `awd groups apply`); a group that exists only in policy
never grants console access. The admin token keeps working as the recovery path.

## 8. Upgrade

Build and push a new image (step 3), put the new digest in `image_digest` in `<ops>/<tenant>.tfvars`, then:

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

## 9. Rotate the signing key

A cell can hold two signing keys at once. `signing_keys` in `<ops>/<tenant>.tfvars` lists labels,
oldest first: the last label is the current key and signs; with two labels, the first is the
previous key. `awd` signs a rollover statement with the previous key, and enrolled machines repin
to the new key on their next sync without re-enrolling. The default is `["k1"]`. Each label gets
its own KMS key, `alias/aw-<tenant>-<label>`.

1. Append a new label (1 to 16 characters of a-z, 0-9 and hyphens) and apply:

   ```hcl
   signing_keys = ["k1", "k2"]
   ```

   ```sh
   tofu -chdir=deploy/aws/cell init -reconfigure \
     -backend-config=bucket=<state-bucket> \
     -backend-config=key=cells/<tenant>.tfstate \
     -backend-config=region=<region>
   tofu -chdir=deploy/aws/cell apply -var-file=<ops>/<tenant>.tfvars
   ```

   The new key signs from now on and `k1` becomes the previous key. The changed environment makes a
   new task definition, so ECS redeploys the service by itself. Check that it settled (step 8),
   and only if the service did not roll, force it:
   `aws ecs update-service --region <region> --cluster aw --service aw-<tenant> --force-new-deployment`.

2. Wait until every machine uses the new key id. The new key id is in the service log:

   ```sh
   aws logs tail /aw/<tenant> --region <region> --since 1h | grep 'signing bundles'
   ```

   Machines sync on their own schedule, so allow at least a day. Then check each machine. On the
   machine, `aw doctor` must print `bundle signature: verified (v2, key <new-key-id>)`. For the whole
   fleet at once, with `AWD_ADMIN_TOKEN` set to the tenant's admin token, the KEY column of
   `awd machines --url https://<tenant>.<domain>` must show the new key id on every row.

3. Only then remove the old label and apply again:

   ```hcl
   signing_keys = ["k2"]
   ```

   Run the same `init -reconfigure` and apply as in step 1. The old key enters AWS's 7-day deletion
   window. A machine that has not repinned by now fails verification and must re-enroll, so do not
   skip the check in step 2.

If a key itself may be compromised, still follow these steps, but go to step 3 as soon as the
machines have moved, and give the partner the CloudTrail `Sign` events for the old key (see
`trust.md`). This procedure has not been exercised on a real account.

## 10. Backup and restore

**Not yet rehearsed against AWS.** Run it once on a throwaway cell before a partner needs it.

RDS keeps automated backups for 7 days with point-in-time restore. Restore into a new instance and
swap it in under the same identifier. The endpoint is expected to stay the same; if it does not,
the plan below updates the database URL secret and the service picks it up on redeploy.

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
```

OpenTofu tracks the instance by its AWS resource id, not by its identifier, so it still points at
the renamed old instance. Re-point it at the restored one before any apply:

```sh
tofu -chdir=deploy/aws/cell init -reconfigure \
  -backend-config=bucket=<state-bucket> \
  -backend-config=key=cells/<tenant>.tfstate \
  -backend-config=region=<region>
tofu -chdir=deploy/aws/cell state rm aws_db_instance.this
tofu -chdir=deploy/aws/cell import -var-file=<ops>/<tenant>.tfvars aws_db_instance.this aw-<tenant>
tofu -chdir=deploy/aws/cell plan -var-file=<ops>/<tenant>.tfvars
```

The plan should show only deletion protection and backup retention being restored, and, if you
scaled the service down to 0 above, `desired_count` going from 0 to 1 on `aws_ecs_service.this`. If the endpoint
changed, the database URL secret version is updated too. If the plan shows anything else, stop and
read it. Then:

```sh
tofu -chdir=deploy/aws/cell apply -var-file=<ops>/<tenant>.tfvars

# Start the service again (a no-op if the apply already set desired_count) and check.
aws ecs update-service --region <region> --cluster aw --service aw-<tenant> --desired-count 1 --force-new-deployment
curl -fsS https://<tenant>.<domain>/readyz

# Once the partner confirms the data, delete the old instance.
aws rds modify-db-instance --region <region> --db-instance-identifier aw-<tenant>-old \
  --no-deletion-protection --apply-immediately
aws rds delete-db-instance --region <region> --db-instance-identifier aw-<tenant>-old \
  --skip-final-snapshot --delete-automated-backups
```

The restored instance keeps the master password (the generated one in state), so no credential
changes.

## 11. Delete a cell

On the partner's written request, or at the end of the pilot. This destroys their data.

Note the key ARNs first, because they are gone from state afterwards:

```sh
tofu -chdir=deploy/aws/cell init -reconfigure \
  -backend-config=bucket=<state-bucket> \
  -backend-config=key=cells/<tenant>.tfstate \
  -backend-config=region=<region>
tofu -chdir=deploy/aws/cell output kms_key_arns
```

Then:

```sh
aws rds modify-db-instance --region <region> --db-instance-identifier aw-<tenant> \
  --no-deletion-protection --apply-immediately
tofu -chdir=deploy/aws/cell destroy -var-file=<ops>/<tenant>.tfvars
```

The first command is needed because the cell keeps deletion protection on. What is and is not removed:

- The RDS instance and its automated backups are deleted with it. No final snapshot is taken.
- Every KMS key of the cell (all `signing_keys` labels) enters a 7-day pending-deletion window; after
  that it is gone for good. To undo inside the window, for each key ARN:
  `aws kms cancel-key-deletion --key-id <arn>` and then `aws kms enable-key --key-id <arn>`
  (cancelling leaves the key disabled).
- The secrets have a 7-day recovery window, during which the same tenant name cannot be created
  again. To reuse the name now, delete them without recovery. `console-client-secret` exists only if
  the cell had a console, so the loop skips secrets that are not there:

```sh
for name in admin-token scim-token database-url console-client-secret; do
  aws secretsmanager describe-secret --region <region> --secret-id aw-<tenant>/$name >/dev/null 2>&1 &&
    aws secretsmanager delete-secret --region <region> --secret-id aw-<tenant>/$name --force-delete-without-recovery
done
```

- The CloudWatch log group `/aw/<tenant>` is deleted with the cell.
- The state file stays, and it holds the old secrets. The state bucket is versioned, and it can also
  hold a `cells/<tenant>.tfstate.tflock` lock object, so list every key and version first:

```sh
aws s3api list-object-versions --bucket <state-bucket> --prefix cells/<tenant>.tfstate \
  --query '[Versions,DeleteMarkers][][Key,VersionId]' --output text
```

Delete each listed pair, for both `cells/<tenant>.tfstate` and `cells/<tenant>.tfstate.tflock`:

```sh
aws s3api delete-object --bucket <state-bucket> --key <key> --version-id <version-id>
```

Check the listing is empty (it prints nothing or `None`). Remove the tenant's tfvars from the ops repository.
