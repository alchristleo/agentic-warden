terraform {
  required_version = ">= 1.13"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 6.70"
    }
  }

  # Partial config: bucket, key and region come from -backend-config.
  # The key must be platform.tfstate: cells read this state under exactly that key.
  backend "s3" {
    use_lockfile = true
    encrypt      = true
  }
}

provider "aws" {
  region = var.region
}
