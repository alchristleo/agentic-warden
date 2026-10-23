variable "region" {
  type        = string
  description = "AWS region for the platform and all cells."
}

variable "domain" {
  type        = string
  description = "Base domain. Its Route 53 hosted zone must already exist."
}

variable "name" {
  type        = string
  description = "Name prefix for platform resources."
  default     = "aw"
}
