mock_provider "aws" {
  mock_data "aws_iam_policy_document" {
    defaults = {
      json = "{\"Version\":\"2012-10-17\",\"Statement\":[]}"
    }
  }

  mock_resource "aws_iam_role" {
    defaults = {
      arn = "arn:aws:iam::123456789012:role/example"
    }
  }

  mock_resource "aws_lb_target_group" {
    defaults = {
      arn = "arn:aws:elasticloadbalancing:eu-west-1:123456789012:targetgroup/example/0123456789abcdef"
    }
  }
}

mock_provider "random" {}

override_data {
  target = data.terraform_remote_state.platform
  values = {
    outputs = {
      vpc_id                = "vpc-0123456789abcdef0"
      public_subnet_ids     = ["subnet-0aaaaaaaaaaaaaaaa", "subnet-0bbbbbbbbbbbbbbbb"]
      alb_security_group_id = "sg-0123456789abcdef0"
      https_listener_arn    = "arn:aws:elasticloadbalancing:eu-west-1:123456789012:listener/app/aw/0123456789abcdef/0123456789abcdef"
      alb_dns_name          = "aw-1234.eu-west-1.elb.amazonaws.com"
      alb_zone_id           = "Z32O12XQLNTSW2"
      ecs_cluster_arn       = "arn:aws:ecs:eu-west-1:123456789012:cluster/aw"
      ecr_repository_url    = "123456789012.dkr.ecr.eu-west-1.amazonaws.com/aw/awd"
      ecr_repository_arn    = "arn:aws:ecr:eu-west-1:123456789012:repository/aw/awd"
      zone_id               = "Z0123456789ABCDEFGHIJ"
      domain                = "example.com"
    }
  }
}

override_data {
  target = data.aws_caller_identity.current
  values = {
    account_id = "123456789012"
  }
}

variables {
  tenant       = "example"
  region       = "eu-west-1"
  state_bucket = "example-aw-tfstate"
  image_digest = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
}

run "database_is_private_encrypted_protected" {
  command = plan

  assert {
    condition     = aws_db_instance.this.publicly_accessible == false
    error_message = "RDS must not be publicly accessible"
  }
  assert {
    condition     = aws_db_instance.this.storage_encrypted == true
    error_message = "RDS storage must be encrypted"
  }
  assert {
    condition     = aws_db_instance.this.deletion_protection == true
    error_message = "RDS deletion protection must be on"
  }
  assert {
    condition     = aws_db_instance.this.backup_retention_period == 7
    error_message = "RDS backups must be kept 7 days"
  }
  assert {
    condition     = aws_db_instance.this.engine == "postgres" && startswith(aws_db_instance.this.engine_version, "17") && aws_db_instance.this.instance_class == "db.t4g.micro"
    error_message = "RDS must be postgres 17 on db.t4g.micro"
  }
  assert {
    condition     = aws_db_instance.this.delete_automated_backups == true && aws_db_instance.this.skip_final_snapshot == true
    error_message = "automated backups are deleted with the instance, no final snapshot"
  }
}

run "database_accepts_only_the_task" {
  command = apply

  assert {
    condition     = length(aws_security_group.db.ingress) == 1 && one(aws_security_group.db.ingress).from_port == 5432 && one(aws_security_group.db.ingress).to_port == 5432
    error_message = "DB SG must have one ingress rule on 5432"
  }
  assert {
    condition     = try(length(one(aws_security_group.db.ingress).cidr_blocks), 0) == 0 && length(one(aws_security_group.db.ingress).security_groups) == 1
    error_message = "DB SG ingress must come from a security group only"
  }
  assert {
    condition     = one(aws_security_group.db.ingress).security_groups == toset([aws_security_group.task.id]) && aws_security_group.task.id != "sg-0123456789abcdef0"
    error_message = "DB SG ingress must come only from the task SG"
  }
}

run "signing_key_spec_and_name" {
  command = plan

  assert {
    condition     = alltrue([for k in aws_kms_key.signing : k.customer_master_key_spec == "ECC_NIST_EDWARDS25519" && k.key_usage == "SIGN_VERIFY"])
    error_message = "KMS key must be Ed25519 SIGN_VERIFY"
  }
  assert {
    condition     = alltrue([for k in aws_kms_key.signing : k.deletion_window_in_days == 7])
    error_message = "KMS deletion window must be 7 days"
  }
  assert {
    condition     = aws_kms_alias.signing["k1"].name == "alias/aw-example-k1" && length(aws_kms_alias.signing) == 1
    error_message = "KMS alias must be alias/aw-<tenant>-<label>"
  }
}

run "key_policy_confines_signing_to_the_task_role" {
  command = plan

  override_resource {
    target = aws_iam_role.task
    values = {
      arn = "arn:aws:iam::123456789012:role/aw-example-task"
    }
  }
  override_resource {
    target = aws_iam_role.execution
    values = {
      arn = "arn:aws:iam::123456789012:role/aw-example-execution"
    }
  }

  assert {
    condition     = length(jsondecode(aws_kms_key.signing["k1"].policy).Statement) == 2
    error_message = "key policy must have exactly the admin allow and the sign deny"
  }
  assert {
    condition     = one([for st in jsondecode(aws_kms_key.signing["k1"].policy).Statement : st if st.Effect == "Allow"]).Principal.AWS == "arn:aws:iam::123456789012:root" && one([for st in jsondecode(aws_kms_key.signing["k1"].policy).Statement : st if st.Effect == "Allow"]).Action == "kms:*"
    error_message = "key policy must let the account root administer the key"
  }
  assert {
    condition     = one([for st in jsondecode(aws_kms_key.signing["k1"].policy).Statement : st if st.Effect == "Deny"]).Action == "kms:Sign" && one([for st in jsondecode(aws_kms_key.signing["k1"].policy).Statement : st if st.Effect == "Deny"]).Principal == "*"
    error_message = "key policy must deny kms:Sign to everyone ..."
  }
  assert {
    condition     = one([for st in jsondecode(aws_kms_key.signing["k1"].policy).Statement : st if st.Effect == "Deny"]).Condition.StringNotEquals["aws:PrincipalArn"] == "arn:aws:iam::123456789012:role/aw-example-task"
    error_message = "... except the cell's task role"
  }
}

run "task_role_signs_only_with_own_key" {
  command = plan

  assert {
    condition     = length(jsondecode(aws_iam_role_policy.task.policy).Statement) == 1
    error_message = "task role must have exactly one statement"
  }
  assert {
    condition     = toset(jsondecode(aws_iam_role_policy.task.policy).Statement[0].Action) == toset(["kms:Sign", "kms:GetPublicKey"])
    error_message = "task role may only call kms:Sign and kms:GetPublicKey"
  }
  assert {
    condition     = toset(jsondecode(aws_iam_role_policy.task.policy).Statement[0].Resource) == toset([aws_kms_key.signing["k1"].arn]) && length(aws_kms_key.signing) == 1
    error_message = "task role must be limited to this cell's key ARN"
  }
  assert {
    condition     = jsondecode(aws_iam_role_policy.task.policy).Statement[0].Effect == "Allow"
    error_message = "task statement must be Allow"
  }
}

run "execution_role_reads_only_own_secrets" {
  command = plan

  assert {
    condition     = toset(jsondecode(aws_iam_role_policy.execution_secrets.policy).Statement[0].Action) == toset(["secretsmanager:GetSecretValue"])
    error_message = "execution role may only read secret values"
  }
  assert {
    condition     = toset(jsondecode(aws_iam_role_policy.execution_secrets.policy).Statement[0].Resource) == toset([aws_secretsmanager_secret.admin_token.arn, aws_secretsmanager_secret.scim_token.arn, aws_secretsmanager_secret.database_url.arn])
    error_message = "execution role must read exactly this cell's secrets"
  }
}

run "execution_role_pulls_only_from_the_platform_repository" {
  command = plan

  assert {
    condition     = one([for st in jsondecode(aws_iam_role_policy.execution_runtime.policy).Statement : st if contains(st.Action, "ecr:BatchGetImage")]).Resource == "arn:aws:ecr:eu-west-1:123456789012:repository/aw/awd"
    error_message = "execution role must pull from exactly the platform ECR repository ARN"
  }
}

run "rotation_with_two_keys" {
  command = plan

  variables {
    signing_keys = ["k1", "k2"]
  }

  override_resource {
    target = aws_kms_key.signing["k1"]
    values = {
      arn = "arn:aws:kms:eu-west-1:123456789012:key/11111111-1111-1111-1111-111111111111"
    }
  }
  override_resource {
    target = aws_kms_key.signing["k2"]
    values = {
      arn = "arn:aws:kms:eu-west-1:123456789012:key/22222222-2222-2222-2222-222222222222"
    }
  }

  assert {
    condition     = { for e in jsondecode(aws_ecs_task_definition.this.container_definitions)[0].environment : e.name => e.value }["AWD_SIGNING_KEY"] == "awskms:arn:aws:kms:eu-west-1:123456789012:key/22222222-2222-2222-2222-222222222222"
    error_message = "current signing key must be the last label"
  }
  assert {
    condition     = { for e in jsondecode(aws_ecs_task_definition.this.container_definitions)[0].environment : e.name => e.value }["AWD_SIGNING_KEY_PREVIOUS"] == "awskms:arn:aws:kms:eu-west-1:123456789012:key/11111111-1111-1111-1111-111111111111"
    error_message = "previous signing key must be the first label"
  }
  assert {
    condition     = toset(jsondecode(aws_iam_role_policy.task.policy).Statement[0].Resource) == toset(["arn:aws:kms:eu-west-1:123456789012:key/11111111-1111-1111-1111-111111111111", "arn:aws:kms:eu-west-1:123456789012:key/22222222-2222-2222-2222-222222222222"])
    error_message = "task role must sign with exactly both cell keys"
  }
}

run "one_key_has_no_previous" {
  command = plan

  assert {
    condition     = !contains([for e in jsondecode(aws_ecs_task_definition.this.container_definitions)[0].environment : e.name], "AWD_SIGNING_KEY_PREVIOUS")
    error_message = "a single key must not set AWD_SIGNING_KEY_PREVIOUS"
  }
}

run "three_keys_rejected" {
  command = plan

  variables {
    signing_keys = ["k1", "k2", "k3"]
  }

  expect_failures = [var.signing_keys]
}

run "duplicate_keys_rejected" {
  command = plan

  variables {
    signing_keys = ["k1", "k1"]
  }

  expect_failures = [var.signing_keys]
}

run "bad_key_label_rejected" {
  command = plan

  variables {
    signing_keys = ["K_1"]
  }

  expect_failures = [var.signing_keys]
}

run "empty_keys_rejected" {
  command = plan

  variables {
    signing_keys = []
  }

  expect_failures = [var.signing_keys]
}

run "task_accepts_only_the_alb" {
  command = plan

  assert {
    condition     = length(aws_security_group.task.ingress) == 1
    error_message = "task SG must have exactly one ingress rule"
  }
  assert {
    condition     = one(aws_security_group.task.ingress).from_port == 8080 && one(aws_security_group.task.ingress).to_port == 8080 && one(aws_security_group.task.ingress).protocol == "tcp"
    error_message = "task SG ingress must be tcp 8080"
  }
  assert {
    condition     = one(aws_security_group.task.ingress).security_groups == toset(["sg-0123456789abcdef0"]) && try(length(one(aws_security_group.task.ingress).cidr_blocks), 0) == 0
    error_message = "task SG ingress must come only from the ALB SG"
  }
}

run "secrets_are_secrets_not_environment" {
  command = plan

  assert {
    condition     = toset([for s in jsondecode(aws_ecs_task_definition.this.container_definitions)[0].secrets : s.name]) == toset(["AWD_DATABASE_URL", "AWD_ADMIN_TOKEN", "AWD_SCIM_TOKEN"])
    error_message = "database URL, admin token and SCIM token must be injected as secrets"
  }
  assert {
    condition     = length(setintersection(toset([for e in jsondecode(aws_ecs_task_definition.this.container_definitions)[0].environment : e.name]), toset(["AWD_DATABASE_URL", "AWD_ADMIN_TOKEN", "AWD_SCIM_TOKEN", "AWD_CONSOLE_CLIENT_SECRET"]))) == 0
    error_message = "secret values must never be plain environment variables"
  }
  assert {
    condition     = !strcontains(aws_ecs_task_definition.this.container_definitions, random_password.admin_token.result) && !strcontains(aws_ecs_task_definition.this.container_definitions, random_password.scim_token.result)
    error_message = "token values must not appear in the task definition"
  }
}

run "environment_values" {
  command = plan

  assert {
    condition     = { for e in jsondecode(aws_ecs_task_definition.this.container_definitions)[0].environment : e.name => e.value } == { AWD_ADDR = ":8080", AWD_SIGNING_KEY = "awskms:${aws_kms_key.signing["k1"].arn}", AWD_LOG_LEVEL = "info" }
    error_message = "environment must be exactly addr, signing key and log level without a console"
  }
  assert {
    condition     = aws_ecs_service.this.desired_count == 1
    error_message = "desired_count must be 1"
  }
  assert {
    condition     = aws_lb_target_group.this.health_check[0].path == "/readyz" && aws_lb_target_group.this.port == 8080
    error_message = "target group must check /readyz on 8080"
  }
  assert {
    condition     = aws_cloudwatch_log_group.this.retention_in_days == 30
    error_message = "logs kept 30 days"
  }
  assert {
    condition     = one(one(aws_lb_listener_rule.this.condition).host_header).values == toset(["example.example.com"])
    error_message = "listener rule must match the tenant host"
  }
  assert {
    condition     = aws_route53_record.this.name == "example.example.com" && aws_route53_record.this.type == "A"
    error_message = "alias record must be <tenant>.<domain>"
  }
}

run "database_url_requires_tls" {
  command = plan

  assert {
    condition     = endswith(aws_secretsmanager_secret_version.database_url.secret_string, "?sslmode=require")
    error_message = "database URL must use sslmode=require"
  }
}

run "console_adds_environment_and_one_secret" {
  command = plan

  variables {
    console = {
      issuer      = "https://idp.example.com"
      client_id   = "awd-example"
      admin_group = "aw-admins"
      user_claim  = "email"
    }
    console_client_secret = "not-a-real-secret"
  }

  assert {
    condition     = contains([for e in jsondecode(aws_ecs_task_definition.this.container_definitions)[0].environment : e.name], "AWD_CONSOLE_ISSUER") && contains([for e in jsondecode(aws_ecs_task_definition.this.container_definitions)[0].environment : e.name], "AWD_CONSOLE_ADMIN_GROUP")
    error_message = "console settings must be set as environment"
  }
  assert {
    condition     = contains([for s in jsondecode(aws_ecs_task_definition.this.container_definitions)[0].secrets : s.name], "AWD_CONSOLE_CLIENT_SECRET")
    error_message = "console client secret must be a secret"
  }
  assert {
    condition     = !contains([for e in jsondecode(aws_ecs_task_definition.this.container_definitions)[0].environment : e.name], "AWD_CONSOLE_CLIENT_SECRET")
    error_message = "console client secret must not be environment"
  }
  assert {
    condition     = { for e in jsondecode(aws_ecs_task_definition.this.container_definitions)[0].environment : e.name => e.value }["AWD_PUBLIC_URL"] == "https://example.example.com"
    error_message = "console needs AWD_PUBLIC_URL"
  }
  assert {
    condition     = length(jsondecode(aws_iam_role_policy.execution_secrets.policy).Statement[0].Resource) == 4
    error_message = "execution role must also read the console secret"
  }
}

run "image_is_pinned_by_digest" {
  command = plan

  assert {
    condition     = jsondecode(aws_ecs_task_definition.this.container_definitions)[0].image == "123456789012.dkr.ecr.eu-west-1.amazonaws.com/aw/awd@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
    error_message = "image must be the platform repository URL pinned by the digest"
  }
}

run "bad_digest_is_rejected" {
  command = plan

  variables {
    image_digest = "latest"
  }

  expect_failures = [var.image_digest]
}

run "short_digest_is_rejected" {
  command = plan

  variables {
    image_digest = "sha256:0123"
  }

  expect_failures = [var.image_digest]
}

run "bad_tenant_is_rejected" {
  command = plan

  variables {
    tenant = "Bad_Tenant"
  }

  expect_failures = [var.tenant]
}

run "console_without_secret_is_rejected" {
  command = plan

  variables {
    console = {
      issuer      = "https://idp.example.com"
      client_id   = "awd-example"
      admin_group = "aw-admins"
    }
  }

  expect_failures = [var.console_client_secret]
}

run "console_user_claim_defaults_to_email" {
  command = plan

  variables {
    console = {
      issuer      = "https://idp.example.com"
      client_id   = "awd-example"
      admin_group = "aw-admins"
    }
    console_client_secret = "not-a-real-secret"
  }

  assert {
    condition     = { for e in jsondecode(aws_ecs_task_definition.this.container_definitions)[0].environment : e.name => e.value }["AWD_CONSOLE_USER_CLAIM"] == "email"
    error_message = "user_claim must default to email"
  }
}

run "tenant_trailing_hyphen_is_rejected" {
  command = plan

  variables {
    tenant = "acme-"
  }

  expect_failures = [var.tenant]
}

run "tenant_double_hyphen_is_rejected" {
  command = plan

  variables {
    tenant = "ac--me"
  }

  expect_failures = [var.tenant]
}

run "tenant_with_inner_hyphen_is_accepted" {
  command = plan

  variables {
    tenant = "ac-me"
  }

  assert {
    condition     = aws_db_instance.this.identifier == "aw-ac-me"
    error_message = "single inner hyphens are valid"
  }
}

run "tenant_over_31_characters_is_rejected" {
  command = plan

  variables {
    tenant = "abcdefghijklmnopqrstuvwxyzabcdef"
  }

  expect_failures = [var.tenant]
}
