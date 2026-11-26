package terraform

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAnalyzePlanJSON(t *testing.T) {
	fixture := `{
  "terraform_version": "1.9.5",
  "resource_changes": [
    {
      "address": "aws_instance.web",
      "mode": "managed",
      "type": "aws_instance",
      "name": "web",
      "provider_name": "registry.terraform.io/hashicorp/aws",
      "change": {
        "actions": ["create"],
        "before": null,
        "after": {"instance_type": "t3.micro", "user_data": "super-secret-script"},
        "after_sensitive": {"user_data": true}
      }
    },
    {
      "address": "aws_s3_bucket.logs",
      "mode": "managed",
      "type": "aws_s3_bucket",
      "name": "logs",
      "provider_name": "registry.terraform.io/hashicorp/aws",
      "change": {
        "actions": ["update"],
        "before": {"acl": "public-read"},
        "after": {"acl": "private"},
        "after_sensitive": false,
        "before_sensitive": false
      }
    },
    {
      "address": "aws_db_instance.old",
      "mode": "managed",
      "type": "aws_db_instance",
      "name": "old",
      "provider_name": "registry.terraform.io/hashicorp/aws",
      "change": {
        "actions": ["delete"],
        "before": {"identifier": "old-db", "password": "hunter2"},
        "after": null,
        "before_sensitive": {"password": true}
      }
    },
    {
      "address": "aws_launch_template.app",
      "mode": "managed",
      "type": "aws_launch_template",
      "name": "app",
      "provider_name": "registry.terraform.io/hashicorp/aws",
      "change": {
        "actions": ["delete", "create"],
        "before": {"image_id": "ami-111"},
        "after": {"image_id": "ami-222"},
        "action_reason": "replace_because_cannot_update"
      }
    },
    {
      "address": "aws_vpc.main",
      "mode": "managed",
      "type": "aws_vpc",
      "name": "main",
      "provider_name": "registry.terraform.io/hashicorp/aws",
      "change": {
        "actions": ["no-op"],
        "before": {"cidr_block": "10.0.0.0/16"},
        "after": {"cidr_block": "10.0.0.0/16"}
      }
    }
  ],
  "output_changes": {
    "endpoint": {"actions": ["create"]}
  }
}`

	res, err := AnalyzePlanJSON([]byte(fixture))
	if err != nil {
		t.Fatalf("AnalyzePlanJSON: %v", err)
	}
	if !res.HasChanges || res.TFVersion != "1.9.5" {
		t.Fatalf("unexpected header: %+v", res)
	}
	if res.Counts["create"] != 1 || res.Counts["update"] != 1 || res.Counts["destroy"] != 1 || res.Counts["replace"] != 1 {
		t.Fatalf("unexpected counts: %+v", res.Counts)
	}
	if len(res.Resources) != 4 {
		t.Fatalf("expected 4 changed resources (no-op excluded), got %d", len(res.Resources))
	}
	wantSymbols := []string{"+create", "~update", "-destroy", "-/+replace"}
	for i, want := range wantSymbols {
		if res.Resources[i].Symbol != want {
			t.Fatalf("resource %d symbol = %q, want %q", i, res.Resources[i].Symbol, want)
		}
	}
	b, _ := json.Marshal(res)
	s := string(b)
	if strings.Contains(s, "super-secret-script") || strings.Contains(s, "hunter2") {
		t.Fatalf("sensitive attributes leaked in PlanAnalysis JSON: %s", s)
	}
	if !strings.Contains(s, "(sensitive)") {
		t.Fatalf("expected (sensitive) placeholder in PlanAnalysis JSON: %s", s)
	}
}
