package terraform

import (
	"strings"
	"testing"
)

func TestSuggestConstraint(t *testing.T) {
	for in, want := range map[string]string{
		"5.31.2":        "~> 5.31",
		"v5.31.2":       "~> 5.31",
		"6.0.0":         "~> 6.0",
		"0.12.3":        "~> 0.12",
		"1.2":           "~> 1.2",
		"3":             "~> 3.0",
		"1.2.3-rc.1":    "= 1.2.3-rc.1",
		"v2.0.0-beta":   "= 2.0.0-beta",
		"1.2.3+build.5": "~> 1.2",
		"":              "",
		"latest":        "",
		"1.2.3\"\nx":    "",
		"1.2.3.4":       "",
		"${evil}":       "",
	} {
		if got := SuggestConstraint(in); got != want {
			t.Errorf("SuggestConstraint(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRegistryModuleSnippet(t *testing.T) {
	info := RegistryModuleDetail{
		Module: RegistryModule{Namespace: "terraform-aws-modules", Name: "vpc", Provider: "aws", Version: "6.7.3"},
		Inputs: []RegistryInput{
			{Name: "name", Type: "string", Description: "VPC name\nsecond line", Required: true},
			{Name: "azs", Type: "list(string)", Description: "AZs", Required: true},
			{Name: "tags", Type: "map(string)", Required: true},
			{Name: "count_", Type: "number", Required: true},
			{Name: "enabled", Type: "bool", Required: true},
			{Name: "cfg", Type: "any", Required: true},
			{Name: "opt", Type: "string", Required: false},
			{Name: "bad name", Type: "string", Required: true},
			{Name: "obj", Type: "object({\n a = string\n})", Description: `has "quote" and ${x}`, Required: true},
			{Name: "evil", Type: "string}\nresource \"x\" \"y\" {", Required: true},
		},
	}
	main, vars := ModuleSnippet(info, "", "", true)
	for _, want := range []string{
		"module \"vpc\" {\n", "  source  = \"terraform-aws-modules/vpc/aws\"\n", "  version = \"~> 6.7\"\n\n",
		"  azs     = []  # AZs\n", "  name    = \"\"  # VPC name\n", "  count_  = 0\n", "  enabled = false\n", "  cfg     = {}\n", "  tags    = {}\n",
	} {
		if !strings.Contains(main, want) {
			t.Errorf("main missing %q in:\n%s", want, main)
		}
	}
	if strings.Contains(main, "opt") || strings.Contains(main, "bad name") || strings.Contains(main, "second line") {
		t.Errorf("optional/invalid inputs or multi-line comment leaked:\n%s", main)
	}
	for _, want := range []string{"variable \"name\" {\n  type        = string\n  description = \"VPC name\"\n}\n", "type        = list(string)",
		"type        = object({ a = string })", `description = "has \"quote\" and $${x}"`} {
		if !strings.Contains(vars, want) {
			t.Errorf("variables missing %q in:\n%s", want, vars)
		}
	}
	if strings.Contains(vars, "resource") || strings.Contains(vars, "opt") || !strings.HasSuffix(vars, "}\n") {
		t.Errorf("variables output wrong:\n%s", vars)
	}
	if _, v := ModuleSnippet(info, "5.0.0", "my vpc!", false); v != "" {
		t.Errorf("variables without declareVars: %q", v)
	}
	if m, _ := ModuleSnippet(info, "5.0.0", "my vpc!", false); !strings.HasPrefix(m, "module \"my_vpc_\" {") || !strings.Contains(m, `"~> 5.0"`) {
		t.Errorf("name/version override:\n%s", m)
	}
	if m, v := ModuleSnippet(RegistryModuleDetail{Module: info.Module}, "", "", true); v != "" || strings.Contains(m, "\n\n") {
		t.Errorf("no inputs:\n%s|%q", m, v)
	}
}

func TestRegistryProviderSnippet(t *testing.T) {
	want := "terraform {\n  required_providers {\n    aws = {\n      source  = \"hashicorp/aws\"\n      version = \"~> 6.72\"\n    }\n  }\n}\n"
	if got := ProviderSnippet("hashicorp", "aws", "6.72.0"); got != want {
		t.Errorf("got:\n%s", got)
	}
	if got := ProviderSnippet("acme", "foo", "1.0.0-rc1"); !strings.Contains(got, `source  = "acme/foo"`) || !strings.Contains(got, `version = "= 1.0.0-rc1"`) {
		t.Errorf("prerelease/community:\n%s", got)
	}
	if got := ProviderSnippet("acme", "foo", ""); strings.Contains(got, "version") {
		t.Errorf("empty version:\n%s", got)
	}
}
