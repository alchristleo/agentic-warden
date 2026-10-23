variable "tenant" {
  type        = string
  description = "Tenant name; the cell is served at <tenant>.<domain>."

  validation {
    condition     = can(regex("^[a-z][a-z0-9-]{1,30}$", var.tenant))
    error_message = "tenant must match ^[a-z][a-z0-9-]{1,30}$."
  }
}

variable "region" {
  type        = string
  description = "AWS region of the platform and the cell."
}

variable "state_bucket" {
  type        = string
  description = "S3 bucket holding the platform state (key platform.tfstate)."
}

variable "image" {
  type        = string
  description = "Image pinned by digest: <ecr_repository_url>@sha256:<hex>."

  validation {
    condition     = strcontains(var.image, "@sha256:")
    error_message = "image must be pinned by digest (contain @sha256:)."
  }
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
