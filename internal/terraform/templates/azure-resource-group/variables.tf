variable "name" {
  type        = string
  description = "Resource group name"
  default     = "pantau-example"
}

variable "location" {
  type        = string
  description = "Azure region"
  default     = "westeurope"
}

variable "tags" {
  type        = map(string)
  description = "Tags applied to the resource group"
  default     = {}
}
