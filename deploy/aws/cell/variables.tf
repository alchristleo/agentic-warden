variable "tenant" {
  type        = string
  description = "Tenant name; the cell is served at <tenant>.<domain>."

  validation {
    condition     = can(regex("^[a-z]([a-z0-9]|-[a-z0-9]){1,30}$", var.tenant)) && length(var.tenant) <= 31
    error_message = "tenant must be 2 to 31 characters of a-z, 0-9 and single inner hyphens, starting with a letter."
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

variable "image_digest" {
  type        = string
  description = "Digest of the awd image in the platform repository, for example sha256:<64 hex>."

  validation {
    condition     = can(regex("^sha256:[0-9a-f]{64}$", var.image_digest))
    error_message = "image_digest must match ^sha256:[0-9a-f]{64}$."
  }
}

variable "console" {
  type = object({
    issuer      = string
    client_id   = string
    admin_group = string
    user_claim  = optional(string, "email")
  })
  default     = null
  description = "OIDC settings for the admin console; null leaves the console disabled."
}

variable "console_client_secret" {
  type        = string
  default     = null
  sensitive   = true
  description = "OIDC client secret for the admin console; required when console is set. Pass it with TF_VAR_console_client_secret."

  validation {
    condition     = var.console == null || var.console_client_secret != null
    error_message = "console_client_secret is required when console is set."
  }
}

variable "signing_keys" {
  type        = list(string)
  default     = ["k1"]
  description = "Labels of the cell's KMS signing keys, oldest first. The last is current; with two, the first is the previous key kept for rotation."

  validation {
    condition     = length(var.signing_keys) >= 1 && length(var.signing_keys) <= 2 && length(distinct(var.signing_keys)) == length(var.signing_keys) && alltrue([for k in var.signing_keys : can(regex("^[a-z0-9-]{1,16}$", k))])
    error_message = "signing_keys needs 1 or 2 distinct labels, each matching ^[a-z0-9-]{1,16}$."
  }
}
