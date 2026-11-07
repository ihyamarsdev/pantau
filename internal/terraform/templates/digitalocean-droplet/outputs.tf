output "droplet_id" {
  description = "ID of the droplet"
  value       = digitalocean_droplet.this.id
}

output "ipv4_address" {
  description = "Public IPv4 address"
  value       = digitalocean_droplet.this.ipv4_address
}
