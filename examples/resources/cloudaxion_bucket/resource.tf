resource "cloudaxion_bucket" "state" {
  name = "neo-tn-prod-tfstate"
}

# Everything inside the bucket is the S3 API's business, not this provider's.
data "cloudaxion_s3_endpoint" "this" {}
