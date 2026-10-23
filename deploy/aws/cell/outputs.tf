output "url" {
  value = "https://${local.host}"
}

output "kms_key_arns" {
  value = { for label, k in aws_kms_key.signing : label => k.arn }
}

output "secret_arns" {
  value = {
    admin_token  = aws_secretsmanager_secret.admin_token.arn
    scim_token   = aws_secretsmanager_secret.scim_token.arn
    database_url = aws_secretsmanager_secret.database_url.arn
  }
}
