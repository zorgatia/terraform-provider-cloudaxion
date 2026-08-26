terraform {
  required_version = ">= 1.9"

  required_providers {
    cloudaxion = {
      source  = "zorgatia/cloudaxion"
      version = ">= 0.1"
    }
    random = {
      source  = "hashicorp/random"
      version = "~> 3.6"
    }
  }
}
