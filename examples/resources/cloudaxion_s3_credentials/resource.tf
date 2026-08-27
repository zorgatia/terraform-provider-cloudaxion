# The secret is returned once, at creation, and never again. It lives in state
# and nowhere else — so treat state as a secret, and recover a lost key by
# issuing a new pair rather than trying to read the old one back.
resource "cloudaxion_s3_credentials" "state" {}

output "s3_access_key" {
  value = cloudaxion_s3_credentials.state.access_key
}

output "s3_secret_key" {
  value     = cloudaxion_s3_credentials.state.secret_key
  sensitive = true
}
