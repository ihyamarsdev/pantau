variable "name" {
  type        = string
  description = "Server name"
  default     = "pantau-example"
}

variable "server_type" {
  type        = string
  description = "Hetzner server type"
  default     = "cx22"
}

variable "image" {
  type        = string
  description = "Server image"
  default     = "ubuntu-24.04"
}

variable "location" {
  type        = string
  description = "Hetzner location"
  default     = "fsn1"
}
