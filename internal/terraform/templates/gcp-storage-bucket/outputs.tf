output "bucket_url" {
  description = "gs:// URL of the bucket"
  value       = google_storage_bucket.this.url
}
