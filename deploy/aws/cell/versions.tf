terraform {
  required_version = ">= 1.13"

  # Partial configuration; supply bucket, region and key at init:
  #   tofu init -backend-config=bucket=<bucket> -backend-config=region=<region> \
  #     -backend-config=key=cells/<tenant>.tfstate
  backend "s3" {
    use_lockfile = true
    encrypt      = true
  }

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 6.70"
    }
    random = {
      source  = "hashicorp/random"
      version = "~> 3.6"
    }
  }
}

provider "aws" {
  region = var.region
}
