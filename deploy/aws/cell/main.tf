data "terraform_remote_state" "platform" {
  backend = "s3"

  config = {
    bucket = var.state_bucket
    key    = "platform.tfstate"
    region = var.region
  }
}

data "aws_region" "current" {}

locals {
  p = data.terraform_remote_state.platform.outputs

  name = "aw-${var.tenant}"
  host = "${var.tenant}.${local.p.domain}"
  port = 8080

  # AWD_PUBLIC_URL is a console setting in awd; set without the console it crash-loops.
  console_env = var.console == null ? [] : [
    { name = "AWD_PUBLIC_URL", value = "https://${local.host}" },
    { name = "AWD_CONSOLE_ISSUER", value = var.console.issuer },
    { name = "AWD_CONSOLE_CLIENT_ID", value = var.console.client_id },
    { name = "AWD_CONSOLE_ADMIN_GROUP", value = var.console.admin_group },
    { name = "AWD_CONSOLE_USER_CLAIM", value = var.console.user_claim },
  ]

  secret_arns = merge(
    {
      AWD_DATABASE_URL = aws_secretsmanager_secret.database_url.arn
      AWD_ADMIN_TOKEN  = aws_secretsmanager_secret.admin_token.arn
      AWD_SCIM_TOKEN   = aws_secretsmanager_secret.scim_token.arn
    },
    var.console == null ? {} : { AWD_CONSOLE_CLIENT_SECRET = aws_secretsmanager_secret.console_client_secret[0].arn },
  )
}

# Signing key

resource "aws_kms_key" "signing" {
  description              = "awd bundle signing key for ${var.tenant}"
  customer_master_key_spec = "ECC_NIST_EDWARDS25519"
  key_usage                = "SIGN_VERIFY"
  deletion_window_in_days  = 7
}

resource "aws_kms_alias" "signing" {
  name          = "alias/${local.name}"
  target_key_id = aws_kms_key.signing.key_id
}

# Secrets

resource "random_password" "admin_token" {
  length  = 48
  special = false
}

resource "random_password" "scim_token" {
  length  = 48
  special = false
}

resource "random_password" "db" {
  length  = 32
  special = false
}

resource "aws_secretsmanager_secret" "admin_token" {
  name                    = "${local.name}/admin-token"
  recovery_window_in_days = 7
}

resource "aws_secretsmanager_secret_version" "admin_token" {
  secret_id     = aws_secretsmanager_secret.admin_token.id
  secret_string = random_password.admin_token.result
}

resource "aws_secretsmanager_secret" "scim_token" {
  name                    = "${local.name}/scim-token"
  recovery_window_in_days = 7
}

resource "aws_secretsmanager_secret_version" "scim_token" {
  secret_id     = aws_secretsmanager_secret.scim_token.id
  secret_string = random_password.scim_token.result
}

resource "aws_secretsmanager_secret" "database_url" {
  name                    = "${local.name}/database-url"
  recovery_window_in_days = 7
}

resource "aws_secretsmanager_secret_version" "database_url" {
  secret_id     = aws_secretsmanager_secret.database_url.id
  secret_string = "postgres://awd:${random_password.db.result}@${aws_db_instance.this.address}:5432/awd?sslmode=require"
}

resource "aws_secretsmanager_secret" "console_client_secret" {
  count = var.console == null ? 0 : 1

  name                    = "${local.name}/console-client-secret"
  recovery_window_in_days = 7
}

resource "aws_secretsmanager_secret_version" "console_client_secret" {
  count = var.console == null ? 0 : 1

  secret_id     = aws_secretsmanager_secret.console_client_secret[0].id
  secret_string = var.console_client_secret
}

# Network access

resource "aws_security_group" "task" {
  name        = "${local.name}-task"
  description = "awd task for ${var.tenant}: inbound only from the ALB"
  vpc_id      = local.p.vpc_id

  ingress {
    description     = "awd from the ALB"
    from_port       = local.port
    to_port         = local.port
    protocol        = "tcp"
    security_groups = [local.p.alb_security_group_id]
  }

  egress {
    description = "database, IdP, ECR, Secrets Manager, KMS"
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
}

resource "aws_security_group" "db" {
  name        = "${local.name}-db"
  description = "awd database for ${var.tenant}: inbound only from the task"
  vpc_id      = local.p.vpc_id

  ingress {
    description     = "postgres from the task"
    from_port       = 5432
    to_port         = 5432
    protocol        = "tcp"
    security_groups = [aws_security_group.task.id]
  }
}

# Database

resource "aws_db_subnet_group" "this" {
  name       = local.name
  subnet_ids = local.p.public_subnet_ids
}

resource "aws_db_instance" "this" {
  identifier = local.name

  engine         = "postgres"
  engine_version = "17"
  instance_class = "db.t4g.micro"

  allocated_storage = 20
  storage_encrypted = true

  db_name  = "awd"
  username = "awd"
  password = random_password.db.result

  db_subnet_group_name   = aws_db_subnet_group.this.name
  vpc_security_group_ids = [aws_security_group.db.id]
  publicly_accessible    = false

  deletion_protection      = true
  backup_retention_period  = 7
  delete_automated_backups = true
  skip_final_snapshot      = true
}

# IAM

data "aws_iam_policy_document" "ecs_tasks_assume" {
  statement {
    actions = ["sts:AssumeRole"]

    principals {
      type        = "Service"
      identifiers = ["ecs-tasks.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "task" {
  name               = "${local.name}-task"
  assume_role_policy = data.aws_iam_policy_document.ecs_tasks_assume.json
}

resource "aws_iam_role_policy" "task" {
  name = "sign"
  role = aws_iam_role.task.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect   = "Allow"
      Action   = ["kms:Sign", "kms:GetPublicKey"]
      Resource = aws_kms_key.signing.arn
    }]
  })
}

resource "aws_iam_role" "execution" {
  name               = "${local.name}-execution"
  assume_role_policy = data.aws_iam_policy_document.ecs_tasks_assume.json
}

resource "aws_iam_role_policy" "execution_secrets" {
  name = "secrets"
  role = aws_iam_role.execution.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect   = "Allow"
      Action   = ["secretsmanager:GetSecretValue"]
      Resource = values(local.secret_arns)
    }]
  })
}

resource "aws_iam_role_policy" "execution_runtime" {
  name = "logs-and-image"
  role = aws_iam_role.execution.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect   = "Allow"
        Action   = ["logs:CreateLogStream", "logs:PutLogEvents"]
        Resource = "${aws_cloudwatch_log_group.this.arn}:*"
      },
      {
        Effect   = "Allow"
        Action   = ["ecr:GetAuthorizationToken"]
        Resource = "*"
      },
      {
        Effect   = "Allow"
        Action   = ["ecr:BatchGetImage", "ecr:GetDownloadUrlForLayer"]
        Resource = local.p.ecr_repository_arn
      },
    ]
  })
}

# Service

resource "aws_cloudwatch_log_group" "this" {
  name              = "/aw/${var.tenant}"
  retention_in_days = 30
}

resource "aws_ecs_task_definition" "this" {
  family                   = local.name
  requires_compatibilities = ["FARGATE"]
  network_mode             = "awsvpc"
  cpu                      = 256
  memory                   = 512
  task_role_arn            = aws_iam_role.task.arn
  execution_role_arn       = aws_iam_role.execution.arn

  container_definitions = jsonencode([{
    name         = "awd"
    image        = "${local.p.ecr_repository_url}@${var.image_digest}"
    essential    = true
    portMappings = [{ containerPort = local.port, protocol = "tcp" }]

    environment = concat([
      { name = "AWD_ADDR", value = ":${local.port}" },
      { name = "AWD_SIGNING_KEY", value = "awskms:${aws_kms_key.signing.arn}" },
      { name = "AWD_LOG_LEVEL", value = "info" },
    ], local.console_env)

    secrets = [for name, arn in local.secret_arns : { name = name, valueFrom = arn }]

    logConfiguration = {
      logDriver = "awslogs"
      options = {
        "awslogs-group"         = aws_cloudwatch_log_group.this.name
        "awslogs-region"        = data.aws_region.current.region
        "awslogs-stream-prefix" = "awd"
      }
    }
  }])
}

resource "aws_lb_target_group" "this" {
  port        = local.port
  protocol    = "HTTP"
  target_type = "ip"
  vpc_id      = local.p.vpc_id

  health_check {
    path    = "/readyz"
    matcher = "200"
  }

  lifecycle {
    create_before_destroy = true
  }
}

resource "aws_lb_listener_rule" "this" {
  listener_arn = local.p.https_listener_arn
  # Stable per tenant; a collision (about 1 in 50000) fails the apply loudly.
  priority = 1 + parseint(substr(md5(var.tenant), 0, 4), 16) % 50000

  action {
    type             = "forward"
    target_group_arn = aws_lb_target_group.this.arn
  }

  condition {
    host_header {
      values = [local.host]
    }
  }
}

resource "aws_ecs_service" "this" {
  name            = local.name
  cluster         = local.p.ecs_cluster_arn
  task_definition = aws_ecs_task_definition.this.arn
  launch_type     = "FARGATE"
  desired_count   = 1

  network_configuration {
    subnets          = local.p.public_subnet_ids
    security_groups  = [aws_security_group.task.id]
    assign_public_ip = true
  }

  load_balancer {
    target_group_arn = aws_lb_target_group.this.arn
    container_name   = "awd"
    container_port   = local.port
  }

  depends_on = [aws_lb_listener_rule.this]
}

resource "aws_route53_record" "this" {
  zone_id = local.p.zone_id
  name    = local.host
  type    = "A"

  alias {
    name                   = local.p.alb_dns_name
    zone_id                = local.p.alb_zone_id
    evaluate_target_health = true
  }
}
