package iacscan

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFiles creates the fixture files of a test.
func writeFiles(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// ruleIDs returns the rule ids of a scan, in order.
func ruleIDs(res Result) []string {
	out := make([]string, 0, len(res.Findings))
	for _, f := range res.Findings {
		out = append(out, f.RuleID)
	}
	return out
}

// hasRule reports whether the scan found a rule.
func hasRule(res Result, id string) bool {
	for _, f := range res.Findings {
		if f.RuleID == id {
			return true
		}
	}
	return false
}

func TestSecretRules(t *testing.T) {
	cases := []struct {
		name  string
		file  string
		body  string
		rule  string
		found bool
	}{
		{"pem private key", "keys/server.pem", "-----BEGIN RSA PRIVATE KEY-----\nMIIabc\n", "secret.pem-private-key", true},
		{"aws access key", "main.tf", "access_key = \"AKIAIOSFODNN7EXAMPLE\"\n", "secret.aws-access-key", true},
		{"github token", "ci.yml", "token: ghp_" + "0123456789abcdefghijklmnopqrstuvwx\n", "secret.github-token", true},
		{"github fine grained token", "ci.yml", "token: github_pat_" + "11ABCDEFG0123456789_abcdefghijklmnop\n", "secret.github-token", true},
		{"gitlab token", "ci.yml", "token = glpat-" + "abcdefghij1234567890\n", "secret.gitlab-token", true},
		{"slack token", "ci.yml", "slack_token: xoxb-" + "123456789012-abcdefghijkl\n", "secret.slack-token", true},
		{"google api key", "app.py", "GOOGLE = \"AIza" + "SyA0123456789abcdefghijklmnopqrstuv\"\n", "secret.google-api-key", true},
		{"stripe live key", "app.py", "stripe = \"sk_live_" + "abcdefghij1234567890\"\n", "secret.stripe-key", true},
		{"jwt", "app.py", "auth = \"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9." + "eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U\"\n", "secret.jwt", true},
		{"generic password", "app.py", "db_password = \"sup3rS3cretValue!\"\n", "secret.hardcoded-value", true},
		{"generic api key", "app.py", "api_key = \"9f8b7c6d5e4f3a2b1c0d\"\n", "secret.hardcoded-value", true},
		{"placeholder password", "app.py", "db_password = \"changeme\"\n", "secret.hardcoded-value", false},
		{"jinja placeholder", "app.yml", "db_password: \"{{ vault_db_password }}\"\n", "secret.hardcoded-value", false},
		{"terraform variable", "main.tf", "db_password = var.db_password\n", "secret.hardcoded-value", false},
		{"ansible vault", "vars.yml", "db_password: $ANSIBLE_VAULT;1.1;AES256\n", "secret.hardcoded-value", false},
		{"env file", ".env", "FOO=bar\n", "secret.env-file", true},
		{"tfstate", "terraform.tfstate", "{}\n", "secret.tfstate", true},
		{"private key file name", "id_rsa", "ssh-rsa AAAA\n", "secret.private-key-file", true},
		{"ignore comment", "app.py", "db_password = \"sup3rS3cretValue!\" # pantau:ignore\n", "secret.hardcoded-value", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := writeFiles(t, map[string]string{tc.file: tc.body})
			res := ScanDir(dir, Options{})
			if got := hasRule(res, tc.rule); got != tc.found {
				t.Fatalf("rule %s: found=%v want %v (rules: %v)", tc.rule, got, tc.found, ruleIDs(res))
			}
		})
	}
}

func TestTerraformRules(t *testing.T) {
	cases := []struct {
		name  string
		body  string
		rule  string
		found bool
	}{
		{"open security group", "resource \"aws_security_group\" \"web\" {\n  ingress {\n    from_port = 22\n    to_port = 22\n    cidr_blocks = [\"0.0.0.0/0\"]\n  }\n}\n", "terraform.open-security-group", true},
		{"open security group ipv6", "resource \"aws_security_group\" \"web\" {\n  ingress {\n    cidr_blocks = [\"::/0\"]\n  }\n}\n", "terraform.open-security-group", true},
		{"closed security group", "resource \"aws_security_group\" \"web\" {\n  ingress {\n    cidr_blocks = [\"10.0.0.0/8\"]\n  }\n}\n", "terraform.open-security-group", false},
		{"public s3 acl", "resource \"aws_s3_bucket\" \"b\" {\n  acl = \"public-read\"\n}\n", "terraform.s3-public-acl", true},
		{"private s3 acl", "resource \"aws_s3_bucket\" \"b\" {\n  acl = \"private\"\n}\n", "terraform.s3-public-acl", false},
		{"s3 public access block off", "resource \"aws_s3_bucket_public_access_block\" \"b\" {\n  block_public_acls = false\n}\n", "terraform.s3-public-access-disabled", true},
		{"unencrypted ebs", "resource \"aws_ebs_volume\" \"v\" {\n  storage_encrypted = false\n}\n", "terraform.unencrypted-storage", true},
		{"encrypted ebs", "resource \"aws_ebs_volume\" \"v\" {\n  storage_encrypted = true\n}\n", "terraform.unencrypted-storage", false},
		{"unencrypted db", "resource \"aws_db_instance\" \"db\" {\n  storage_encrypted = false\n}\n", "terraform.unencrypted-storage", true},
		{"public database", "resource \"aws_db_instance\" \"db\" {\n  publicly_accessible = true\n}\n", "terraform.public-database", true},
		{"private database", "resource \"aws_db_instance\" \"db\" {\n  publicly_accessible = false\n}\n", "terraform.public-database", false},
		{"provider credentials", "provider \"aws\" {\n  access_key = \"AKIAIOSFODNN7EXAMPLE\"\n}\n", "terraform.provider-credentials", true},
		{"skip tls verify", "provider \"aws\" {\n  skip_tls_verify = true\n}\n", "terraform.insecure-skip-tls", true},
		{"insecure flag", "resource \"aws_s3_bucket\" \"b\" {\n  insecure = true\n}\n", "terraform.insecure-flag", true},
		{"iam wildcard", "data \"aws_iam_policy_document\" \"p\" {\n  statement {\n    actions = [\"*\"]\n    resources = [\"*\"]\n  }\n}\n", "terraform.iam-wildcard-policy", true},
		{"unpinned provider", "provider \"aws\" {\n  source = \"hashicorp/aws\"\n}\n", "terraform.unpinned-provider", true},
		{"pinned provider", "provider \"aws\" {\n  source = \"hashicorp/aws\"\n  version = \"~> 5.0\"\n}\n", "terraform.unpinned-provider", false},
		{"unpinned module", "module \"vpc\" {\n  source = \"terraform-aws-modules/vpc/aws\"\n}\n", "terraform.unpinned-module", true},
		{"pinned module", "module \"vpc\" {\n  source = \"terraform-aws-modules/vpc/aws\"\n  version = \"5.0.0\"\n}\n", "terraform.unpinned-module", false},
		{"module branch ref", "module \"vpc\" {\n  source = \"git::https://example.com/vpc.git?ref=main\"\n  version = \"1.0.0\"\n}\n", "terraform.unpinned-module", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := writeFiles(t, map[string]string{"main.tf": tc.body})
			res := ScanDir(dir, Options{})
			if got := hasRule(res, tc.rule); got != tc.found {
				t.Fatalf("rule %s: found=%v want %v (rules: %v)", tc.rule, got, tc.found, ruleIDs(res))
			}
		})
	}
}

func TestAnsibleRules(t *testing.T) {
	cases := []struct {
		name  string
		body  string
		rule  string
		found bool
	}{
		{"validate certs off", "- hosts: all\n  tasks:\n    - name: t\n      ansible.builtin.uri:\n        validate_certs: no\n", "ansible.validate-certs-disabled", true},
		{"validate certs on", "- hosts: all\n  tasks:\n    - name: t\n      ansible.builtin.uri:\n        validate_certs: yes\n", "ansible.validate-certs-disabled", false},
		{"curl pipe sh", "- hosts: all\n  tasks:\n    - name: t\n      shell: curl -s https://example.com/i.sh | sh\n", "ansible.remote-script-pipe", true},
		{"permissive mode", "- hosts: all\n  tasks:\n    - name: t\n      ansible.builtin.file:\n        mode: '0777'\n", "ansible.permissive-mode", true},
		{"permissive mode 666", "- hosts: all\n  tasks:\n    - name: t\n      ansible.builtin.file:\n        mode: '0666'\n", "ansible.permissive-mode", true},
		{"state latest", "- hosts: all\n  tasks:\n    - name: t\n      ansible.builtin.package:\n        state: latest\n", "ansible.unpinned-package", true},
		{"ignore errors with become", "- hosts: all\n  tasks:\n    - name: t\n      become: yes\n      ignore_errors: yes\n      shell: /bin/true\n", "ansible.ignore-errors-with-become", true},
		{"strict host key off", "- hosts: all\n  tasks:\n    - name: t\n      shell: ssh -o StrictHostKeyChecking=no host\n", "ansible.strict-host-key-disabled", true},
		{"inventory password", "[web]\nhost ansible_ssh_pass=sup3rS3cretValue!\n", "ansible.inventory-password", true},
		{"password without no_log", "- hosts: all\n  tasks:\n    - name: t\n      ansible.builtin.user:\n        password: sup3rS3cretValue!\n", "ansible.password-without-no-log", true},
		{"password with no_log", "- hosts: all\n  tasks:\n    - name: t\n      no_log: true\n      ansible.builtin.user:\n        password: sup3rS3cretValue!\n", "ansible.password-without-no-log", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := writeFiles(t, map[string]string{"play.yml": tc.body})
			res := ScanDir(dir, Options{})
			if got := hasRule(res, tc.rule); got != tc.found {
				t.Fatalf("rule %s: found=%v want %v (rules: %v)", tc.rule, got, tc.found, ruleIDs(res))
			}
		})
	}
}

func TestDockerRules(t *testing.T) {
	cases := []struct {
		name  string
		file  string
		body  string
		rule  string
		found bool
	}{
		{"latest tag", "Dockerfile", "FROM ubuntu:latest\nUSER app\n", "dockerfile.latest-tag", true},
		{"pinned tag", "Dockerfile", "FROM ubuntu:22.04\nUSER app\n", "dockerfile.latest-tag", false},
		{"no user", "Dockerfile", "FROM ubuntu:22.04\nCMD [\"/bin/sh\"]\n", "dockerfile.no-user", true},
		{"with user", "Dockerfile", "FROM ubuntu:22.04\nUSER app\n", "dockerfile.no-user", false},
		{"add from url", "Dockerfile", "FROM ubuntu:22.04\nUSER app\nADD https://example.com/x.tar /x\n", "dockerfile.add-from-url", true},
		{"curl pipe sh", "Dockerfile", "FROM ubuntu:22.04\nUSER app\nRUN curl -s https://example.com/i.sh | sh\n", "dockerfile.curl-pipe-shell", true},
		{"secret in env", "Dockerfile", "FROM ubuntu:22.04\nUSER app\nENV DB_PASSWORD=sup3rS3cretValue!\n", "dockerfile.secret-in-env", true},
		{"placeholder in env", "Dockerfile", "FROM ubuntu:22.04\nUSER app\nENV DB_PASSWORD=changeme\n", "dockerfile.secret-in-env", false},
		{"privileged compose", "docker-compose.yml", "services:\n  web:\n    privileged: true\n", "dockerfile.privileged-container", true},
		{"docker sock", "docker-compose.yml", "services:\n  web:\n    volumes:\n      - /var/run/docker.sock:/var/run/docker.sock\n", "dockerfile.docker-sock-mount", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := writeFiles(t, map[string]string{tc.file: tc.body})
			res := ScanDir(dir, Options{})
			if got := hasRule(res, tc.rule); got != tc.found {
				t.Fatalf("rule %s: found=%v want %v (rules: %v)", tc.rule, got, tc.found, ruleIDs(res))
			}
		})
	}
}

// TestSecretRedaction is the important one: the raw secret must never appear in the result JSON.
func TestSecretRedaction(t *testing.T) {
	const secret = "AKIAIOSFODNN7EXAMPLE"
	const password = "sup3rS3cretValue!"
	dir := writeFiles(t, map[string]string{
		"main.tf":       "provider \"aws\" {\n  access_key = \"" + secret + "\"\n}\n",
		"app.py":        "db_password = \"" + password + "\"\n",
		"deploy.sh":     "export TOKEN=ghp_" + "0123456789abcdefghijklmnopqrstuvwx\n",
		"settings.yaml": "api_key: 9f8b7c6d5e4f3a2b1c0d\n",
	})
	res := ScanDir(dir, Options{})
	raw, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{secret, password, "ghp_" + "0123456789abcdefghijklmnopqrstuvwx", "9f8b7c6d5e4f3a2b1c0d"} {
		if strings.Contains(string(raw), s) {
			t.Fatalf("the raw secret %q leaked into the result JSON: %s", s, raw)
		}
	}
	if !strings.Contains(string(raw), "AKIA****") {
		t.Fatalf("the redacted value is missing: %s", raw)
	}
}

func TestScanDirSkipsAndLimits(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"main.tf":           "provider \"aws\" {}\n",
		".git/config":       "secret = \"AKIAIOSFODNN7EXAMPLE\"\n",
		".terraform/x":      "secret = \"AKIAIOSFODNN7EXAMPLE\"\n",
		"node_modules/x.js": "secret = \"AKIAIOSFODNN7EXAMPLE\"\n",
		"vendor/x.go":       "secret = \"AKIAIOSFODNN7EXAMPLE\"\n",
	})
	res := ScanDir(dir, Options{})
	if res.FilesScanned != 1 {
		t.Fatalf("expected only main.tf to be scanned, got %d files", res.FilesScanned)
	}

	// A big file is skipped and the result is partial.
	big := writeFiles(t, map[string]string{"big.tf": strings.Repeat("a", 2<<20)})
	res = ScanDir(big, Options{})
	if res.FilesScanned != 0 {
		t.Fatalf("expected the big file to be skipped, got %d files", res.FilesScanned)
	}
	if res.Status != "partial" {
		t.Fatalf("expected status partial, got %q", res.Status)
	}
}

func TestScanDirSkipsSymlinks(t *testing.T) {
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.tf"), []byte("access_key = \"AKIAIOSFODNN7EXAMPLE\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	dir := writeFiles(t, map[string]string{"main.tf": "provider \"aws\" {}\n"})
	if err := os.Symlink(filepath.Join(outside, "secret.tf"), filepath.Join(dir, "link.tf")); err != nil {
		t.Skipf("symlinks not supported here: %v", err)
	}
	res := ScanDir(dir, Options{})
	if hasRule(res, "secret.aws-access-key") {
		t.Fatal("the scanner followed a symlink out of the root")
	}
	if res.Status != "partial" {
		t.Fatalf("expected status partial (skipped symlink), got %q", res.Status)
	}
}

func TestScanDirFindingsCap(t *testing.T) {
	body := ""
	for range 20 {
		body += "access_key = \"AKIAIOSFODNN7EXAMPLE\"\n"
	}
	dir := writeFiles(t, map[string]string{"main.tf": body})
	res := ScanDir(dir, Options{MaxFindings: 5})
	if len(res.Findings) != 5 {
		t.Fatalf("expected 5 findings, got %d", len(res.Findings))
	}
	if !res.Truncated {
		t.Fatal("expected truncated = true")
	}
}

func TestScanDirMissingDirectory(t *testing.T) {
	res := ScanDir(filepath.Join(t.TempDir(), "nope"), Options{})
	if res.Status != "not_assessed" {
		t.Fatalf("expected status not_assessed, got %q", res.Status)
	}
	if len(res.Notes) == 0 {
		t.Fatal("expected a note about the unreadable directory")
	}
}

func TestRedact(t *testing.T) {
	cases := map[string]string{
		"AKIAIOSFODNN7EXAMPLE": "AKIA****",
		"short":                "shor****",
		"abc":                  "****",
		"":                     "****",
	}
	for in, want := range cases {
		if got := Redact(in); got != want {
			t.Errorf("Redact(%q) = %q, want %q", in, got, want)
		}
	}
}
