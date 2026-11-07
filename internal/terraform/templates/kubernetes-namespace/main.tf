terraform {
  required_version = ">= 1.4"

  required_providers {
    kubernetes = {
      source  = "hashicorp/kubernetes"
      version = "~> 2.0"
    }
  }
}

# Cluster access comes from the linked Kubernetes cloud account (KUBECONFIG).
provider "kubernetes" {}

resource "kubernetes_namespace" "this" {
  metadata {
    name   = var.name
    labels = var.labels
  }
}
