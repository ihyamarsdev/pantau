terraform {
  required_version = ">= 1.4"
}

resource "terraform_data" "hello" {
  input = "Hello, ${var.name}!"
}
