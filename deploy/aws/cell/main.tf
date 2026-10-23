data "terraform_remote_state" "platform" {
  backend = "s3"

  config = {
    bucket = var.state_bucket
    key    = "platform.tfstate"
    region = var.region
  }
}

module "cell" {
  source = "../modules/cell"

  tenant  = var.tenant
  image   = var.image
  console = var.console

  vpc_id                = data.terraform_remote_state.platform.outputs.vpc_id
  public_subnet_ids     = data.terraform_remote_state.platform.outputs.public_subnet_ids
  alb_security_group_id = data.terraform_remote_state.platform.outputs.alb_security_group_id
  https_listener_arn    = data.terraform_remote_state.platform.outputs.https_listener_arn
  alb_dns_name          = data.terraform_remote_state.platform.outputs.alb_dns_name
  alb_zone_id           = data.terraform_remote_state.platform.outputs.alb_zone_id
  ecs_cluster_arn       = data.terraform_remote_state.platform.outputs.ecs_cluster_arn
  ecr_repository_url    = data.terraform_remote_state.platform.outputs.ecr_repository_url
  zone_id               = data.terraform_remote_state.platform.outputs.zone_id
  domain                = data.terraform_remote_state.platform.outputs.domain
}
