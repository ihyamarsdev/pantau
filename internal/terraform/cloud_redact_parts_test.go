package terraform

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func redactThrough(secrets []string, text string) string {
	var buf bytes.Buffer
	_, _ = newRedactionWriter(&buf, secrets).Write([]byte(text))
	return buf.String()
}

func TestRedactSecretParts(t *testing.T) {
	kube := `apiVersion: v1
clusters:
- name: prod
  cluster:
    server: https://k8s.example.com:6443
    certificate-authority-data: Q0EtUFVCTElD
contexts:
- name: ctx-main
  context: {cluster: prod, user: admin}
current-context: ctx-main
users:
- name: admin
  user:
    token: kube-token-xyz-123456
    client-key-data: "Q0xJRU5ULUtFWS1EQVRB"
    password: 'hunter2hunter2'
`
	pem := "-----BEGIN PRIVATE KEY-----\nMIIEvQIBADANBgkq\n-----END PRIVATE KEY-----\n"
	gcpBytes, _ := json.Marshal(map[string]string{
		"type": "service_account", "client_email": "sa@proj.iam.gserviceaccount.com",
		"private_key": pem, "private_key_id": "abcdef0123456789",
	})
	gcp := string(gcpBytes)

	cases := []struct {
		name    string
		cfg     Config
		output  string
		masked  []string // must not appear
		visible []string // must still appear
	}{
		{
			name:    "kubeconfig partial print",
			cfg:     Config{EnvFiles: map[string]string{"KUBECONFIG": kube}},
			output:  "    token: kube-token-xyz-123456\nclient-key-data: Q0xJRU5ULUtFWS1EQVRB\npassword: hunter2hunter2\nserver: https://k8s.example.com:6443\ncurrent-context: ctx-main\nca: Q0EtUFVCTElD\n",
			masked:  []string{"kube-token-xyz-123456", "Q0xJRU5ULUtFWS1EQVRB", "hunter2hunter2"},
			visible: []string{"https://k8s.example.com:6443", "current-context: ctx-main", "Q0EtUFVCTElD", "token: (sensitive)"},
		},
		{
			name:    "kubeconfig full print",
			cfg:     Config{EnvFiles: map[string]string{"KUBECONFIG": kube}},
			output:  kube,
			masked:  []string{"kube-token-xyz-123456", "apiVersion: v1"},
			visible: []string{"(sensitive)"},
		},
		{
			name:    "gcp json parts",
			cfg:     Config{Env: map[string]string{"GOOGLE_CREDENTIALS": gcp, "GOOGLE_PROJECT": "my-project"}, PlainEnv: map[string]bool{"GOOGLE_PROJECT": true}},
			output:  "key id abcdef0123456789\nraw " + strings.TrimSpace(pem) + "\nescaped " + strings.ReplaceAll(pem, "\n", `\n`) + "\nproject my-project\nemail sa@proj.iam.gserviceaccount.com\n",
			masked:  []string{"abcdef0123456789", "BEGIN PRIVATE KEY"},
			visible: []string{"project my-project", "sa@proj.iam.gserviceaccount.com"},
		},
		{
			name:    "short value not masked",
			cfg:     Config{EnvFiles: map[string]string{"KUBECONFIG": "token: abc\nname: keepme\n"}},
			output:  "token: abc name: keepme",
			visible: []string{"token: abc", "name: keepme"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := redactThrough(c.cfg.logSecrets(), c.output)
			for _, m := range c.masked {
				if strings.Contains(out, m) {
					t.Errorf("leaks %q:\n%s", m, out)
				}
			}
			for _, v := range c.visible {
				if !strings.Contains(out, v) {
					t.Errorf("lost %q:\n%s", v, out)
				}
			}
		})
	}
}
