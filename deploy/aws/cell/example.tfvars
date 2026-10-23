# Placeholder values only. Real tenant tfvars live in the private ops repository.
tenant       = "example"
region       = "eu-west-1"
state_bucket = "example-aw-tfstate"
image_digest = "sha256:0000000000000000000000000000000000000000000000000000000000000000"

# Optional admin console; omit to leave it disabled. Pass the client secret with
# TF_VAR_console_client_secret, never in a tfvars file.
# console = {
#   issuer      = "https://idp.example.com"
#   client_id   = "awd-example"
#   admin_group = "aw-admins"
#   user_claim  = "email" # optional, default "email"
# }
