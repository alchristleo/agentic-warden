variable "tenant" {
  type        = string
  description = "Tenant name; becomes the host <tenant>.<domain> and part of every resource name."

  validation {
    condition     = can(regex("^[a-z][a-z0-9-]{1,30}$", var.tenant))
    error_message = "tenant must match ^[a-z][a-z0-9-]{1,30}$."
  }
}

variable "image" {
  type        = string
  description = "Container image pinned by digest, for example <ecr_repository_url>@sha256:<hex>."

  validation {
    condition     = strcontains(var.image, "@sha256:")
    error_message = "image must be pinned by digest (contain @sha256:)."
  }
}

variable "vpc_id" {
  type = string
}

variable "public_subnet_ids" {
  type = list(string)
}

variable "alb_security_group_id" {
  type = string
}

variable "https_listener_arn" {
  type = string
}

variable "alb_dns_name" {
  type = string
}

variable "alb_zone_id" {
  type = string
}

variable "ecs_cluster_arn" {
  type = string
}

variable "ecr_repository_url" {
  type = string
}

variable "zone_id" {
  type = string
}

variable "domain" {
  type = string
}

variable "console" {
  type = object({
    issuer        = string
    client_id     = string
    client_secret = string
    admin_group   = string
    user_claim    = string
  })
  default     = null
  sensitive   = true
  description = "OIDC settings for the admin console; null leaves the console disabled."
}
