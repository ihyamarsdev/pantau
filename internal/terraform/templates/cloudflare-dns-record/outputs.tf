output "record_id" {
  description = "ID of the DNS record"
  value       = cloudflare_dns_record.this.id
}
