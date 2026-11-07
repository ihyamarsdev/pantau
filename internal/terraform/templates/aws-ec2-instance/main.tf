terraform {
  required_version = ">= 1.4"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 6.0"
    }
  }
}

# Credentials come from AWS_ACCESS_KEY_ID / AWS_SECRET_ACCESS_KEY (cloud account).
provider "aws" {
  region = var.region
}

data "aws_ssm_parameter" "ami" {
  name = "/aws/service/ami-amazon-linux-latest/al2023-ami-kernel-default-x86_64"
}

resource "aws_instance" "this" {
  ami           = data.aws_ssm_parameter.ami.value
  instance_type = var.instance_type

  tags = {
    Name = var.name
  }
}
