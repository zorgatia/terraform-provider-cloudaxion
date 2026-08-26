terraform {
  required_providers {
    cloudaxion = {
      source  = "zorgatia/cloudaxion"
      version = "~> 0.1"
    }
  }
}

# Credentials come from the environment:
#   CLOUDAXION_API_KEY            API token created in the CloudAxion web interface
#   CLOUDAXION_LOCATION           default location slug
#   CLOUDAXION_BILLING_ACCOUNT_ID billing account charged for created resources
provider "cloudaxion" {}

# Or set them explicitly. Keep api_key out of version control.
provider "cloudaxion" {
  alias              = "explicit"
  location           = "tun1"
  billing_account_id = 12
}
