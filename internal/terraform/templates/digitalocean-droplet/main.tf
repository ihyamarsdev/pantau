terraform {
  required_version = ">= 1.4"

  required_providers {
    digitalocean = {
      source  = "digitalocean/digitalocean"
      version = "~> 2.0"
    }
  }
}

# Credentials come from DIGITALOCEAN_TOKEN (cloud account).
provider "digitalocean" {}

resource "digitalocean_droplet" "this" {
  name   = var.name
  region = var.region
  size   = var.size
  image  = var.image
}
