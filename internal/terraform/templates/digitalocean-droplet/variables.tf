variable "name" {
  type        = string
  description = "Droplet name"
  default     = "pantau-example"
}

variable "region" {
  type        = string
  description = "DigitalOcean region slug"
  default     = "fra1"
}

variable "size" {
  type        = string
  description = "Droplet size slug"
  default     = "s-1vcpu-512mb-10gb"
}

variable "image" {
  type        = string
  description = "Droplet image slug"
  default     = "ubuntu-24-04-x64"
}
