variable "name" {
  type        = string
  description = "Namespace name"
  default     = "pantau-example"
}

variable "labels" {
  type        = map(string)
  description = "Labels applied to the namespace"
  default     = {}
}
