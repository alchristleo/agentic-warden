output "vpc_id" {
  value = aws_vpc.main.id
}

output "public_subnet_ids" {
  value = aws_subnet.public[*].id
}

output "alb_security_group_id" {
  value = aws_security_group.alb.id
}

output "https_listener_arn" {
  value = aws_lb_listener.https.arn
}

output "alb_dns_name" {
  value = aws_lb.main.dns_name
}

output "alb_zone_id" {
  value = aws_lb.main.zone_id
}

output "ecs_cluster_arn" {
  value = aws_ecs_cluster.main.arn
}

output "ecr_repository_url" {
  value = aws_ecr_repository.awd.repository_url
}

output "zone_id" {
  value = data.aws_route53_zone.main.zone_id
}

output "domain" {
  value = var.domain
}
