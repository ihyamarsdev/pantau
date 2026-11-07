package terraform

import (
	"encoding/json"
	"fmt"
	"strings"
)

// maxCloudFieldBytes caps one credential value.
const maxCloudFieldBytes = 16 << 10

// CloudField describes one credential input of a cloud provider.
type CloudField struct {
	Key         string   `json:"key"`
	Label       string   `json:"label"`
	Placeholder string   `json:"placeholder"`
	Default     string   `json:"default"`
	Secret      bool     `json:"secret"`
	Required    bool     `json:"required"`
	Multiline   bool     `json:"multiline"`
	Env         []string `json:"-"` // env vars set from this field
}

// CloudProvider is a cloud whose credentials can be injected into terraform runs.
type CloudProvider struct {
	ID     string       `json:"id"`
	Label  string       `json:"label"`
	Fields []CloudField `json:"fields"`
}

func cloudProviders() []CloudProvider {
	return []CloudProvider{
		{ID: "aws", Label: "Amazon Web Services", Fields: []CloudField{
			{Key: "access_key_id", Label: "Access key ID", Placeholder: "AKIA...", Required: true, Env: []string{"AWS_ACCESS_KEY_ID"}},
			{Key: "secret_access_key", Label: "Secret access key", Secret: true, Required: true, Env: []string{"AWS_SECRET_ACCESS_KEY"}},
			{Key: "session_token", Label: "Session token (optional)", Secret: true, Env: []string{"AWS_SESSION_TOKEN"}},
			{Key: "region", Label: "Default region", Placeholder: "us-east-1", Default: "us-east-1", Env: []string{"AWS_DEFAULT_REGION", "AWS_REGION"}},
		}},
		{ID: "gcp", Label: "Google Cloud", Fields: []CloudField{
			{Key: "project_id", Label: "Project ID", Required: true, Env: []string{"GOOGLE_PROJECT", "GOOGLE_CLOUD_PROJECT"}},
			{Key: "region", Label: "Region", Placeholder: "us-central1", Env: []string{"GOOGLE_REGION"}},
			{Key: "credentials_json", Label: "Service account key (JSON)", Secret: true, Required: true, Multiline: true, Env: []string{"GOOGLE_CREDENTIALS"}},
		}},
		{ID: "azure", Label: "Microsoft Azure", Fields: []CloudField{
			{Key: "subscription_id", Label: "Subscription ID", Required: true, Env: []string{"ARM_SUBSCRIPTION_ID"}},
			{Key: "tenant_id", Label: "Tenant ID", Required: true, Env: []string{"ARM_TENANT_ID"}},
			{Key: "client_id", Label: "Client ID", Required: true, Env: []string{"ARM_CLIENT_ID"}},
			{Key: "client_secret", Label: "Client secret", Secret: true, Required: true, Env: []string{"ARM_CLIENT_SECRET"}},
		}},
		{ID: "digitalocean", Label: "DigitalOcean", Fields: []CloudField{
			{Key: "token", Label: "API token", Secret: true, Required: true, Env: []string{"DIGITALOCEAN_TOKEN"}},
		}},
		{ID: "hetzner", Label: "Hetzner Cloud", Fields: []CloudField{
			{Key: "token", Label: "API token", Secret: true, Required: true, Env: []string{"HCLOUD_TOKEN"}},
		}},
	}
}

// CloudProviders returns the supported providers in stable order.
func CloudProviders() []CloudProvider { return cloudProviders() }

// CloudProviderByID looks a provider up by its id.
func CloudProviderByID(id string) (CloudProvider, bool) {
	for _, p := range cloudProviders() {
		if p.ID == id {
			return p, true
		}
	}
	return CloudProvider{}, false
}

// ValidateCloudFields checks provider id, keys, required values and sizes.
func ValidateCloudFields(provider string, fields map[string]string) error {
	p, ok := CloudProviderByID(provider)
	if !ok {
		return fmt.Errorf("unknown cloud provider %q", provider)
	}
	known := make(map[string]bool, len(p.Fields))
	for _, f := range p.Fields {
		known[f.Key] = true
	}
	for k, v := range fields {
		if !known[k] {
			return fmt.Errorf("unknown field %q for provider %s", k, provider)
		}
		if len(v) > maxCloudFieldBytes {
			return fmt.Errorf("field %q is too large (max %d KiB)", k, maxCloudFieldBytes>>10)
		}
	}
	vals := cloudValues(p, fields)
	for _, f := range p.Fields {
		if f.Required && vals[f.Key] == "" {
			return fmt.Errorf("field %q is required", f.Label)
		}
	}
	if raw := vals["credentials_json"]; provider == "gcp" {
		var sa map[string]any
		if err := json.Unmarshal([]byte(raw), &sa); err != nil {
			return fmt.Errorf("credentials_json is not valid JSON")
		}
		for _, k := range []string{"type", "client_email", "private_key"} {
			if s, _ := sa[k].(string); s == "" {
				return fmt.Errorf("credentials_json is missing %q", k)
			}
		}
	}
	return nil
}

// cloudValues returns the non-empty field values: trimmed (single-line fields) with defaults applied.
func cloudValues(p CloudProvider, fields map[string]string) map[string]string {
	out := make(map[string]string, len(p.Fields))
	for _, f := range p.Fields {
		v := fields[f.Key]
		if f.Multiline {
			if strings.TrimSpace(v) == "" {
				v = ""
			}
		} else {
			v = strings.TrimSpace(v)
		}
		if v == "" {
			v = f.Default
		}
		if v != "" {
			out[f.Key] = v
		}
	}
	return out
}

// CloudEnv maps provider fields to environment variables; empty values are skipped.
func CloudEnv(provider string, fields map[string]string) map[string]string {
	env := map[string]string{}
	p, ok := CloudProviderByID(provider)
	if !ok {
		return env
	}
	vals := cloudValues(p, fields)
	for _, f := range p.Fields {
		if v := vals[f.Key]; v != "" {
			for _, e := range f.Env {
				env[e] = v
			}
		}
	}
	return env
}
