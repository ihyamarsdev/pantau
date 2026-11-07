terraform {
  required_version = ">= 1.4"

  required_providers {
    cloudflare = {
      source  = "cloudflare/cloudflare"
      version = "~> 5.0"
    }
  }
}

# Credentials come from CLOUDFLARE_API_TOKEN (cloud account).
provider "cloudflare" {}

resource "cloudflare_dns_record" "this" {
  zone_id = var.zone_id
  name    = var.name
  type    = var.type
  content = var.content
  ttl     = var.ttl
  proxied = var.proxied
}
