variable "zone_id" {
  type        = string
  description = "Cloudflare zone ID"
}

variable "name" {
  type        = string
  description = "Record name, e.g. app.example.com"
}

variable "type" {
  type        = string
  description = "Record type"
  default     = "A"
}

variable "content" {
  type        = string
  description = "Record value, e.g. an IPv4 address"
}

variable "ttl" {
  type        = number
  description = "TTL in seconds (1 = automatic)"
  default     = 1
}

variable "proxied" {
  type        = bool
  description = "Proxy the record through Cloudflare"
  default     = false
}
