variable "project" {
  type        = string
  description = "GCP project ID (null uses the cloud account project)"
  default     = null
}

variable "bucket_name" {
  type        = string
  description = "Globally unique bucket name"
}

variable "location" {
  type        = string
  description = "Bucket location"
  default     = "US"
}
