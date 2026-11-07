variable "region" {
  type        = string
  description = "AWS region"
  default     = "us-east-1"
}

variable "bucket_name" {
  type        = string
  description = "Globally unique S3 bucket name"
}

variable "tags" {
  type        = map(string)
  description = "Tags applied to the bucket"
  default     = {}
}
