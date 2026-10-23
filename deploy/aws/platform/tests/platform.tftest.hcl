mock_provider "aws" {
  mock_data "aws_availability_zones" {
    defaults = {
      names = ["us-east-1a", "us-east-1b", "us-east-1c"]
    }
  }

  mock_resource "aws_lb" {
    defaults = {
      arn = "arn:aws:elasticloadbalancing:us-east-1:111111111111:loadbalancer/app/aw/0123456789abcdef"
    }
  }

  mock_resource "aws_acm_certificate" {
    defaults = {
      arn = "arn:aws:acm:us-east-1:111111111111:certificate/00000000-0000-0000-0000-000000000000"
    }
  }

  mock_data "aws_route53_zone" {
    defaults = {
      zone_id = "Z0EXAMPLE"
    }
  }
}

variables {
  region = "us-east-1"
  domain = "example.com"
}

run "https_listener" {
  command = plan

  assert {
    condition     = aws_lb_listener.https.port == 443 && aws_lb_listener.https.protocol == "HTTPS"
    error_message = "listener must be HTTPS on 443"
  }
  assert {
    condition     = aws_lb_listener.https.ssl_policy == "ELBSecurityPolicy-TLS13-1-2-2021-06"
    error_message = "TLS policy must be TLS13-1-2-2021-06"
  }
  assert {
    condition     = aws_acm_certificate.wildcard.domain_name == "*.example.com" && aws_acm_certificate.wildcard.validation_method == "DNS"
    error_message = "wildcard cert with DNS validation expected"
  }
  assert {
    condition     = aws_lb_listener.https.default_action[0].type == "fixed-response" && aws_lb_listener.https.default_action[0].fixed_response[0].status_code == "404"
    error_message = "default action must be a fixed 404"
  }
}

run "http_redirect" {
  command = plan

  assert {
    condition     = aws_lb_listener.http.port == 80 && aws_lb_listener.http.default_action[0].type == "redirect"
    error_message = "port 80 must redirect"
  }
  assert {
    condition     = aws_lb_listener.http.default_action[0].redirect[0].port == "443" && aws_lb_listener.http.default_action[0].redirect[0].protocol == "HTTPS" && aws_lb_listener.http.default_action[0].redirect[0].status_code == "HTTP_301"
    error_message = "redirect must go to HTTPS 443"
  }
}

run "alb_sg_inbound" {
  command = plan

  assert {
    condition     = length(aws_vpc_security_group_ingress_rule.alb) == 2 && toset([for r in aws_vpc_security_group_ingress_rule.alb : r.from_port]) == toset([80, 443])
    error_message = "ALB SG must allow inbound only 80 and 443"
  }
  assert {
    condition     = alltrue([for r in aws_vpc_security_group_ingress_rule.alb : r.from_port == r.to_port && r.ip_protocol == "tcp"])
    error_message = "ingress rules must be single-port tcp"
  }
  assert {
    condition     = alltrue([for r in aws_vpc_security_group_ingress_rule.alb : r.cidr_ipv4 == "0.0.0.0/0"])
    error_message = "ALB ingress must be open to 0.0.0.0/0"
  }
}

run "registry_and_subnets" {
  command = plan

  assert {
    condition     = aws_ecr_repository.awd.image_tag_mutability == "IMMUTABLE" && aws_ecr_repository.awd.image_scanning_configuration[0].scan_on_push
    error_message = "ECR must be immutable with scan on push"
  }
  assert {
    condition     = length(aws_subnet.public) == 2
    error_message = "two public subnets expected"
  }
}

run "outputs_present" {
  command = plan

  assert {
    condition     = output.domain == "example.com" && output.zone_id == "Z0EXAMPLE"
    error_message = "domain and zone_id outputs"
  }
  assert {
    condition     = length(output.public_subnet_ids) == 2
    error_message = "public_subnet_ids output"
  }
  assert {
    condition = alltrue([for o in [
      output.vpc_id,
      output.public_subnet_ids,
      output.alb_security_group_id,
      output.https_listener_arn,
      output.alb_dns_name,
      output.alb_zone_id,
      output.ecs_cluster_arn,
      output.ecr_repository_url,
      output.ecr_repository_arn,
      output.zone_id,
      output.domain,
    ] : o != null])
    error_message = "every platform output must be set"
  }
}
