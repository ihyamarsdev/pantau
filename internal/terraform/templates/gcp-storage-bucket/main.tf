terraform {
  required_version = ">= 1.4"

  required_providers {
    google = {
      source  = "hashicorp/google"
      version = "~> 6.0"
    }
  }
}

# Credentials come from GOOGLE_CREDENTIALS (cloud account); project falls back to GOOGLE_PROJECT.
provider "google" {
  project = var.project
}

resource "google_storage_bucket" "this" {
  name                        = var.bucket_name
  location                    = var.location
  uniform_bucket_level_access = true
  force_destroy               = false
}
