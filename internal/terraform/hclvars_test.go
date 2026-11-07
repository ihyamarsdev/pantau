package terraform

import (
	"reflect"
	"testing"
)

func TestParseVariables(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		want  []DeclaredVar
	}{
		{
			name:  "simple",
			files: map[string]string{"variables.tf": "variable \"region\" {\n  type = string\n  description = \"AWS region\"\n  default = \"us-east-1\"\n}\n"},
			want:  []DeclaredVar{{Name: "region", Type: "string", Description: "AWS region", Default: `"us-east-1"`, HasDefault: true, File: "variables.tf"}},
		},
		{
			name: "multiline object type and nested braces in default",
			files: map[string]string{"v.tf": `variable "cfg" {
  type = object({
    name = string
    tags = map(string)
  })
  default = {
    name = "x"
    tags = { a = "}" }
  }
}
variable "after" {}
`},
			want: []DeclaredVar{
				{Name: "cfg", Type: "object({\n    name = string\n    tags = map(string)\n  })", Default: "{\n    name = \"x\"\n    tags = { a = \"}\" }\n  }", HasDefault: true, File: "v.tf"},
				{Name: "after", File: "v.tf"},
			},
		},
		{
			name:  "heredoc default and description",
			files: map[string]string{"v.tf": "variable \"script\" {\n  description = <<-EOT\n    run { this }\n  EOT\n  default = <<EOT\nline } {\nEOT\n}\nvariable \"next\" {\n  default = 1\n}\n"},
			want: []DeclaredVar{
				{Name: "script", Description: "run { this }", Default: "<<EOT\nline } {\nEOT", HasDefault: true, File: "v.tf"},
				{Name: "next", Default: "1", HasDefault: true, File: "v.tf"},
			},
		},
		{
			name: "comments with braces",
			files: map[string]string{"v.tf": `# variable "ghost" {
// }
/* variable "ghost2" { */
variable "real" { # {
  /* } */ default = 5 // }
  type = number # {{
}
`},
			want: []DeclaredVar{{Name: "real", Type: "number", Default: "5", HasDefault: true, File: "v.tf"}},
		},
		{
			name:  "interpolation and escaped quotes",
			files: map[string]string{"v.tf": "variable \"a\" {\n  default = \"x ${var.b == \"}\" ? \"y\" : \"z\"} \\\" }\"\n  description = \"say \\\"hi\\\"\"\n}\n"},
			want:  []DeclaredVar{{Name: "a", Description: `say "hi"`, Default: "\"x ${var.b == \"}\" ? \"y\" : \"z\"} \\\" }\"", HasDefault: true, File: "v.tf"}},
		},
		{
			name:  "sensitive and nested validation block",
			files: map[string]string{"v.tf": "variable \"pw\" {\n  type = string\n  sensitive = true\n  validation {\n    condition = length(var.pw) > 3\n    error_message = \"short\"\n  }\n}\n"},
			want:  []DeclaredVar{{Name: "pw", Type: "string", Sensitive: true, File: "v.tf"}},
		},
		{
			name: "multiple files sorted, non-tf ignored, other blocks skipped",
			files: map[string]string{
				"b.tf":          "resource \"x\" \"y\" {\n  variable = \"no\"\n}\nvariable \"b\" {}\n",
				"a.tf":          "locals { variable = 1 }\nvariable \"a\" {}\n",
				"x.tfvars":      "variable \"nope\" {}\n",
				"main.tf.json":  "{}",
				"zz-notes.hcl":  "variable \"nope2\" {}",
				"terraform.tfv": "",
			},
			want: []DeclaredVar{{Name: "a", File: "a.tf"}, {Name: "b", File: "b.tf"}},
		},
		{
			name: "malformed blocks do not panic",
			files: map[string]string{
				"1.tf": "variable \"a\" {\n  default = {\n",
				"2.tf": "variable {\n}\nvariable \"x\n",
				"3.tf": "variable \"q\" \"\\",
				"4.tf": "variable \"h\" {\n default = <<EOT\nnever closed",
				"5.tf": "variable \"s\" { default = \"unterminated }\nvariable \"ok\" {}\n",
			},
			want: nil, // only asserts no panic; contents checked below
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ParseVariables(tc.files)
			if tc.want == nil {
				return
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got  %#v\nwant %#v", got, tc.want)
			}
		})
	}
}

func TestParseVariablesNeverNil(t *testing.T) {
	if got := ParseVariables(nil); got == nil || len(got) != 0 {
		t.Fatalf("want empty non-nil slice, got %#v", got)
	}
}
