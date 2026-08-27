data "cloudaxion_s3_endpoint" "this" {}

# Point the aws provider at CloudAxion for object-level operations.
provider "aws" {
  endpoints { s3 = data.cloudaxion_s3_endpoint.this.endpoint }

  skip_credentials_validation = true
  skip_region_validation      = true
  skip_requesting_account_id  = true
}
