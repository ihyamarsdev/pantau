output "instance_id" {
  description = "ID of the instance"
  value       = aws_instance.this.id
}

output "public_ip" {
  description = "Public IPv4 address"
  value       = aws_instance.this.public_ip
}
