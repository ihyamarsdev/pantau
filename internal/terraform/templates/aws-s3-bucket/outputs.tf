output "bucket_arn" {
  description = "ARN of the bucket"
  value       = aws_s3_bucket.this.arn
}

output "bucket_name" {
  description = "Name of the bucket"
  value       = aws_s3_bucket.this.id
}
