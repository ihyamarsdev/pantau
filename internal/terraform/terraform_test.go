package terraform

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestStateParseDiffRollback(t *testing.T) {
	stateA := `{
  "version": 4,
  "terraform_version": "1.5.0",
  "serial": 1,
  "lineage": "lineage-uuid-1234",
  "outputs": {
    "my_out": {
      "value": "secret123",
      "type": "string",
      "sensitive": true
    }
  },
  "resources": [
    {
      "mode": "managed",
      "type": "local_file",
      "name": "foo",
      "provider": "provider[\"registry.terraform.io/hashicorp/local\"]",
      "instances": [
        {
          "schema_version": 0,
          "attributes": {
            "content": "hello",
            "filename": "hello.txt",
            "sensitive_key": "topsecret"
          },
          "sensitive_attributes": ["sensitive_key"]
        }
      ]
    }
  ]
}`

	info, err := ParseState([]byte(stateA))
	if err != nil {
		t.Fatalf("ParseState failed: %v", err)
	}
	if info.Serial != 1 || info.Lineage != "lineage-uuid-1234" {
		t.Errorf("unexpected serial/lineage: %d / %s", info.Serial, info.Lineage)
	}
	if len(info.Outputs) != 1 || !info.Outputs[0].Sensitive {
		t.Errorf("expected 1 sensitive output, got: %+v", info.Outputs)
	}
	if len(info.Resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(info.Resources))
	}
	r := info.Resources[0]
	if r.Address != "local_file.foo" {
		t.Errorf("expected address local_file.foo, got %s", r.Address)
	}
	if r.Attributes["sensitive_key"] != "(sensitive)" {
		t.Errorf("expected sensitive_key to be masked, got: %v", r.Attributes["sensitive_key"])
	}

	stateB := `{
  "version": 4,
  "terraform_version": "1.5.0",
  "serial": 2,
  "lineage": "lineage-uuid-1234",
  "outputs": {},
  "resources": [
    {
      "mode": "managed",
      "type": "local_file",
      "name": "foo",
      "instances": [
        {
          "attributes": {
            "content": "world",
            "filename": "hello.txt"
          }
        }
      ]
    },
    {
      "mode": "managed",
      "type": "local_file",
      "name": "bar",
      "instances": [
        {
          "attributes": {
            "content": "new"
          }
        }
      ]
    }
  ]
}`

	diff, err := DiffStates([]byte(stateA), []byte(stateB))
	if err != nil {
		t.Fatalf("DiffStates failed: %v", err)
	}
	if len(diff.Added) != 1 || diff.Added[0] != "local_file.bar" {
		t.Errorf("expected local_file.bar added, got: %v", diff.Added)
	}
	if len(diff.Changed) != 1 || diff.Changed[0].Address != "local_file.foo" {
		t.Errorf("expected local_file.foo changed, got: %v", diff.Changed)
	}

	rolledBack, err := RollbackState([]byte(stateA), []byte(stateB))
	if err != nil {
		t.Fatalf("RollbackState failed: %v", err)
	}
	rbInfo, err := ParseState(rolledBack)
	if err != nil {
		t.Fatalf("ParseState on rolledBack failed: %v", err)
	}
	if rbInfo.Serial != 3 {
		t.Errorf("expected serial 3, got %d", rbInfo.Serial)
	}
	if rbInfo.Lineage != "lineage-uuid-1234" {
		t.Errorf("expected lineage lineage-uuid-1234, got %s", rbInfo.Lineage)
	}
}

func TestPolicyEvaluation(t *testing.T) {
	planJSON := `{
  "format_version": "1.2",
  "resource_changes": [
    {
      "address": "aws_security_group_rule.allow_all",
      "mode": "managed",
      "type": "aws_security_group_rule",
      "name": "allow_all",
      "change": {
        "actions": ["create"],
        "after": {
          "cidr_blocks": ["0.0.0.0/0"],
          "from_port": 22,
          "to_port": 22
        }
      }
    },
    {
      "address": "aws_instance.web",
      "mode": "managed",
      "type": "aws_instance",
      "name": "web",
      "change": {
        "actions": ["delete"],
        "before": {
          "id": "i-12345"
        }
      }
    }
  ]
}`

	policies := []Policy{
		{
			ID:          1,
			Name:        "No Open CIDR",
			Kind:        "deny_open_cidr",
			Enforcement: "hard",
		},
		{
			ID:          2,
			Name:        "Deny Destroy",
			Kind:        "deny_destroy",
			Enforcement: "advisory",
		},
		{
			ID:          3,
			Name:        "Require Tags",
			Kind:        "require_tags",
			Enforcement: "soft",
			Params:      json.RawMessage(`{"tags":["Environment","Owner"]}`),
		},
	}

	report, err := EvaluatePolicies([]byte(planJSON), policies)
	if err != nil {
		t.Fatalf("EvaluatePolicies failed: %v", err)
	}
	if report.Status != "failed" {
		t.Errorf("expected report.Status=failed due to hard deny_open_cidr, got: %s", report.Status)
	}
	if len(report.Results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(report.Results))
	}
	if report.Results[0].Passed {
		t.Errorf("expected deny_open_cidr to fail")
	}
	if report.Results[1].Passed {
		t.Errorf("expected deny_destroy to fail")
	}
	if report.Results[2].Passed {
		t.Errorf("expected require_tags to fail")
	}
}

func TestModuleParser(t *testing.T) {
	files := map[string]string{
		"README.md": "# My Module\nDocumentation here.",
		"main.tf": `
terraform {
  required_version = ">= 1.3.0"
}

provider "aws" {
  region = "us-east-1"
}

resource "aws_s3_bucket" "b" {
  bucket = var.bucket_name
}
`,
		"variables.tf": `
variable "bucket_name" {
  type        = string
  description = "The S3 bucket name"
}

variable "secret_key" {
  type        = string
  description = "A secret token"
  sensitive   = true
  default     = "none"
}
`,
		"outputs.tf": `
output "bucket_arn" {
  description = "ARN of bucket"
  value       = aws_s3_bucket.b.arn
}
`,
	}

	doc := ParseModule(files)
	if !strings.Contains(doc.Readme, "Documentation here") {
		t.Errorf("unexpected readme: %s", doc.Readme)
	}
	if doc.RequiredVersion != ">= 1.3.0" {
		t.Errorf("unexpected required_version: %s", doc.RequiredVersion)
	}
	if len(doc.Inputs) != 2 {
		t.Fatalf("expected 2 inputs, got %d", len(doc.Inputs))
	}
	if doc.Inputs[0].Name != "bucket_name" || !doc.Inputs[0].Required {
		t.Errorf("expected bucket_name to be required input: %+v", doc.Inputs[0])
	}
	if doc.Inputs[1].Name != "secret_key" || !doc.Inputs[1].Sensitive || doc.Inputs[1].Required {
		t.Errorf("expected secret_key sensitive and optional: %+v", doc.Inputs[1])
	}
	if len(doc.Outputs) != 1 || doc.Outputs[0].Name != "bucket_arn" {
		t.Errorf("expected bucket_arn output: %+v", doc.Outputs)
	}
	if len(doc.Resources) != 1 || doc.Resources[0] != "aws_s3_bucket.b" {
		t.Errorf("expected aws_s3_bucket.b resource: %v", doc.Resources)
	}
}

func TestWebhookVerification(t *testing.T) {
	secret := "mysecretkey12345"
	body := []byte(`{"ref":"refs/heads/feature/cool-stuff"}`)

	// GitHub style
	h := make(http.Header)
	h.Set("X-Hub-Signature-256", "sha256=f5540b078565ad370db83e6ea3478de3eb172b03f7b0184291c7b78e525d89f6")
	branch, err := ParseWebhook(h, body, secret)
	if err != nil {
		t.Fatalf("GitHub webhook failed: %v", err)
	}
	if branch != "feature/cool-stuff" {
		t.Errorf("expected branch feature/cool-stuff, got: %s", branch)
	}

	// GitLab style
	h2 := make(http.Header)
	h2.Set("X-Gitlab-Token", secret)
	branch2, err := ParseWebhook(h2, body, secret)
	if err != nil {
		t.Fatalf("GitLab webhook failed: %v", err)
	}
	if branch2 != "feature/cool-stuff" {
		t.Errorf("expected branch feature/cool-stuff, got: %s", branch2)
	}

	// Invalid signature
	hBad := make(http.Header)
	hBad.Set("X-Hub-Signature-256", "sha256=0000000000000000000000000000000000000000000000000000000000000000")
	if _, err := ParseWebhook(hBad, body, secret); err == nil {
		t.Errorf("expected invalid signature to fail")
	}
}

func TestValidationAndVarWriting(t *testing.T) {
	cfg := Config{
		Binary:   "terraform",
		DataRoot: "/tmp/pantau/tf",
		Root:     "/tmp/pantau/tf/ws-1",
		Files: map[string]string{
			"main.tf": "resource \"terraform_data\" \"test\" {}",
			"bad..tf": "invalid",
		},
	}
	if err := cfg.Validate(); err == nil {
		t.Errorf("expected invalid file name with .. to fail")
	}

	cfg.Files = map[string]string{
		"main.tf": "resource \"terraform_data\" \"test\" {}",
	}
	cfg.Vars = []Var{
		{Key: "normal_var", Value: "val1", HCL: false},
		{Key: "bad key with space", Value: "val2", HCL: false},
	}
	if err := cfg.Validate(); err == nil {
		t.Errorf("expected invalid var key to fail")
	}

	cfg.Vars = []Var{
		{Key: "normal_var", Value: "val1", HCL: false},
		{Key: "hcl_var", Value: `["a", "b"]`, HCL: true},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected valid config, got: %v", err)
	}

	tmpDir := t.TempDir()
	if err := writeVarFiles(tmpDir, cfg.Vars); err != nil {
		t.Fatalf("writeVarFiles failed: %v", err)
	}
}
