package inspector

import (
	"encoding/json"
	"strings"
	"testing"
)

// FuzzCheckKubeconfigPolicy feeds arbitrary kubeconfig JSON through the policy that gates every
// external cluster. An accepted config must never carry a local file credential, an auth-provider block,
// or an exec plugin outside the allowlist.
func FuzzCheckKubeconfigPolicy(f *testing.F) {
	f.Add([]byte(`{"clusters":[{"cluster":{"server":"https://10.0.0.1:6443"}}],"users":[{"user":{"token":"x"}}]}`))
	f.Add([]byte(`{"clusters":[{"cluster":{"certificate-authority":"/etc/ca.pem"}}]}`))
	f.Add([]byte(`{"users":[{"user":{"exec":{"command":"aws","args":["eks"]}}}]}`))
	f.Add([]byte(`{"users":[{"user":{"exec":{"command":"/bin/sh"}}}]}`))
	f.Add([]byte(`{"users":[{"user":{"tokenFile":"/root/.token"}}]}`))
	f.Add([]byte(`{"users":[{"user":{"auth-provider":{"name":"gcp"}}}]}`))
	f.Fuzz(func(t *testing.T, raw []byte) {
		var parsed kubeconfigJSON
		if err := json.Unmarshal(raw, &parsed); err != nil {
			t.Skip()
		}
		server, execCmd, err := checkKubeconfigPolicy(parsed)
		_ = deriveProvider(execCmd, server)
		if err != nil {
			return
		}
		for _, c := range parsed.Clusters {
			if c.Cluster.CertificateAuthority != "" {
				t.Fatalf("accepted a file-path certificate-authority: %q", c.Cluster.CertificateAuthority)
			}
		}
		for _, u := range parsed.Users {
			if u.User.ClientCertificate != "" || u.User.ClientKey != "" || u.User.TokenFile != "" || len(u.User.AuthProvider) > 0 {
				t.Fatalf("accepted a local file credential or auth-provider: %+v", u.User)
			}
			if u.User.Exec != nil {
				name := u.User.Exec.Command
				if strings.ContainsAny(name, `/\`) || !allowedExecCommands[name] {
					t.Fatalf("accepted exec plugin outside the allowlist: %q", name)
				}
			}
		}
		if execCmd != "" && !allowedExecCommands[execCmd] {
			t.Fatalf("returned exec command outside the allowlist: %q", execCmd)
		}
	})
}
