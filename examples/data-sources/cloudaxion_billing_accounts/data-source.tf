# Nearly every create call needs a billing account id.
data "cloudaxion_billing_accounts" "all" {}

output "default_billing_account" {
  value = data.cloudaxion_billing_accounts.all.default_id
}
