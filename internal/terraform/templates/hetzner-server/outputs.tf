output "server_id" {
  description = "ID of the server"
  value       = hcloud_server.this.id
}

output "ipv4_address" {
  description = "Public IPv4 address"
  value       = hcloud_server.this.ipv4_address
}
