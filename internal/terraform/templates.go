package terraform

import (
	"embed"
	"path"
	"strconv"
)

//go:embed templates
var templateFS embed.FS

// TemplateVar is a variable of a template as shown in the picker.
type TemplateVar struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Default     string `json:"default"`
}

// Template is a built-in workspace starter.
type Template struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Provider    string            `json:"provider"` // aws|gcp|azure|digitalocean|hetzner|cloudflare|kubernetes|none
	Variables   []TemplateVar     `json:"variables"`
	Files       map[string]string `json:"files"`
}

// The .tf sources live in templates/<id>/; variables are derived from variables.tf.
var templateMeta = []Template{
	{ID: "hello-world", Name: "Hello world", Provider: "none", Description: "Offline starter using the builtin terraform_data resource; no provider or credentials needed."},
	{ID: "aws-s3-bucket", Name: "AWS S3 bucket", Provider: "aws", Description: "Private S3 bucket with public access blocked."},
	{ID: "aws-ec2-instance", Name: "AWS EC2 instance", Provider: "aws", Description: "Single t3.micro instance on the latest Amazon Linux 2023 AMI."},
	{ID: "gcp-storage-bucket", Name: "GCP storage bucket", Provider: "gcp", Description: "Cloud Storage bucket with uniform bucket-level access."},
	{ID: "azure-resource-group", Name: "Azure resource group", Provider: "azure", Description: "A resource group, the base for other Azure resources."},
	{ID: "digitalocean-droplet", Name: "DigitalOcean droplet", Provider: "digitalocean", Description: "Smallest droplet running Ubuntu 24.04."},
	{ID: "hetzner-server", Name: "Hetzner Cloud server", Provider: "hetzner", Description: "Small Hetzner Cloud server running Ubuntu 24.04."},
	{ID: "cloudflare-dns-record", Name: "Cloudflare DNS record", Provider: "cloudflare", Description: "A single DNS record in an existing zone."},
	{ID: "kubernetes-namespace", Name: "Kubernetes namespace", Provider: "kubernetes", Description: "A namespace in the cluster of the linked kubeconfig."},
}

// Templates returns the built-in templates in display order.
// Each call builds fresh copies, so callers may mutate the result.
func Templates() []Template {
	out := make([]Template, 0, len(templateMeta))
	for _, m := range templateMeta {
		dir := path.Join("templates", m.ID)
		entries, err := templateFS.ReadDir(dir)
		if err != nil {
			continue // unreachable: guarded by TestTemplates_*
		}
		m.Files = map[string]string{}
		for _, e := range entries {
			if b, err := templateFS.ReadFile(path.Join(dir, e.Name())); err == nil {
				m.Files[e.Name()] = string(b)
			}
		}
		m.Variables = []TemplateVar{}
		for _, v := range ParseVariables(m.Files) {
			d := v.Default
			if u, err := strconv.Unquote(d); err == nil {
				d = u
			}
			m.Variables = append(m.Variables, TemplateVar{Name: v.Name, Description: v.Description, Default: d})
		}
		out = append(out, m)
	}
	return out
}
