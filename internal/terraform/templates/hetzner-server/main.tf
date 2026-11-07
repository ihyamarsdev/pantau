terraform {
  required_version = ">= 1.4"

  required_providers {
    hcloud = {
      source  = "hetznercloud/hcloud"
      version = "~> 1.50"
    }
  }
}

# Credentials come from HCLOUD_TOKEN (cloud account).
provider "hcloud" {}

resource "hcloud_server" "this" {
  name        = var.name
  server_type = var.server_type
  image       = var.image
  location    = var.location
}
