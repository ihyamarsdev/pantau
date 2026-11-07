output "greeting" {
  description = "The rendered greeting"
  value       = terraform_data.hello.output
}
