output "namespace" {
  description = "Name of the namespace"
  value       = kubernetes_namespace.this.metadata[0].name
}
