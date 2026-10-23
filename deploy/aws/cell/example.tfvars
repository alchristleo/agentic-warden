# Placeholder values only. Real tenant tfvars live in the private ops repository.
tenant       = "example"
region       = "eu-west-1"
state_bucket = "example-aw-tfstate"
image        = "123456789012.dkr.ecr.eu-west-1.amazonaws.com/aw@sha256:0000000000000000000000000000000000000000000000000000000000000000"

# Optional admin console; omit to leave it disabled. Pass the secret with
# TF_VAR_console or a private tfvars file, never in this repository.
# console = {
#   issuer        = "https://idp.example.com"
#   client_id     = "awd-example"
#   client_secret = "set-in-private-tfvars"
#   admin_group   = "aw-admins"
#   user_claim    = "email"
# }
