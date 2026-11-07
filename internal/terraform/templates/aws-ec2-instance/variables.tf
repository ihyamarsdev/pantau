variable "region" {
  type        = string
  description = "AWS region"
  default     = "us-east-1"
}

variable "name" {
  type        = string
  description = "Name tag of the instance"
  default     = "pantau-example"
}

variable "instance_type" {
  type        = string
  description = "EC2 instance type"
  default     = "t3.micro"
}
