package teleport

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Status is the Teleport session Pantau found for one profile.
type Status struct {
	Active     bool
	Username   string
	ProxyURL   string
	Cluster    string
	ValidUntil time.Time
}

// TTL is how long the certificate stays valid after now. It is zero or negative once the session expired.
func (s Status) TTL(now time.Time) time.Duration {
	if !s.Active {
		return 0
	}
	return s.ValidUntil.Sub(now)
}

// KubeCluster is one Kubernetes cluster tsh lists for a Teleport profile.
type KubeCluster struct {
	Name   string
	Labels map[string]string
}

type tshProfile struct {
	ProxyURL   string    `json:"profile_url"`
	Username   string    `json:"username"`
	Cluster    string    `json:"cluster"`
	ValidUntil time.Time `json:"valid_until"`
}

type tshStatus struct {
	Active   *tshProfile  `json:"active"`
	Profiles []tshProfile `json:"profiles"`
}

// ParseStatus reads `tsh status --format=json` and picks the profile for proxy (and cluster, when set).
// It searches the active profile first. When no profile matches it returns an inactive Status, not an error.
func ParseStatus(raw []byte, proxy, cluster string) (Status, error) {
	var doc tshStatus
	if err := json.Unmarshal(raw, &doc); err != nil {
		return Status{}, fmt.Errorf("parse tsh status: %w", err)
	}
	candidates := make([]tshProfile, 0, len(doc.Profiles)+1)
	if doc.Active != nil {
		candidates = append(candidates, *doc.Active)
	}
	candidates = append(candidates, doc.Profiles...)
	for _, c := range candidates {
		if !sameProxy(c.ProxyURL, proxy) {
			continue
		}
		if cluster != "" && c.Cluster != cluster {
			continue
		}
		return Status{Active: true, Username: c.Username, ProxyURL: c.ProxyURL, Cluster: c.Cluster, ValidUntil: c.ValidUntil}, nil
	}
	return Status{}, nil
}

// sameProxy compares a profile URL ("https://host:443") with a configured proxy ("host:443" or "host").
// A missing port means 443, the Teleport default.
func sameProxy(profileURL, proxy string) bool {
	want, ok := normalizeHostPort(proxy)
	if !ok {
		return false
	}
	got, ok := normalizeHostPort(profileURL)
	return ok && got == want
}

func normalizeHostPort(raw string) (string, bool) {
	s := strings.TrimSpace(raw)
	if strings.Contains(s, "://") {
		u, err := url.Parse(s)
		if err != nil {
			return "", false
		}
		s = u.Host
	}
	if s == "" {
		return "", false
	}
	host, port, err := net.SplitHostPort(s)
	if err != nil {
		host, port = strings.Trim(s, "[]"), "443"
	}
	if host == "" {
		return "", false
	}
	return strings.ToLower(host) + ":" + port, true
}

type tshKubeItem struct {
	Name       string          `json:"kube_cluster_name"`
	LegacyName string          `json:"name"`
	Labels     json.RawMessage `json:"labels"`
}

// ParseKubeLS reads `tsh kube ls --format=json`. Entries whose name fails ValidateKubeName are dropped, so a
// name the runner could not pass to `tsh kube login` never reaches the database. The result is sorted by name.
func ParseKubeLS(raw []byte) ([]KubeCluster, error) {
	var items []tshKubeItem
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, fmt.Errorf("parse tsh kube ls: %w", err)
	}
	out := make([]KubeCluster, 0, len(items))
	seen := make(map[string]bool, len(items))
	for _, it := range items {
		name := it.Name
		if name == "" {
			name = it.LegacyName
		}
		if ValidateKubeName(name) != nil || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, KubeCluster{Name: name, Labels: parseLabels(it.Labels)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// parseLabels accepts a JSON object or a "k=v,k2=v2" string. Anything else yields no labels.
func parseLabels(raw json.RawMessage) map[string]string {
	labels := map[string]string{}
	if len(raw) == 0 {
		return labels
	}
	var obj map[string]string
	if err := json.Unmarshal(raw, &obj); err == nil {
		return obj
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		for _, pair := range strings.Split(s, ",") {
			k, v, ok := strings.Cut(strings.TrimSpace(pair), "=")
			if ok && k != "" {
				labels[k] = v
			}
		}
	}
	return labels
}

type kubeconfigDoc struct {
	CurrentContext string `yaml:"current-context"`
	Contexts       []struct {
		Name string `yaml:"name"`
	} `yaml:"contexts"`
}

// ParseCurrentContext returns the current-context of an isolated kubeconfig, after checking that the name is
// valid and that a context of that name exists.
func ParseCurrentContext(raw []byte) (string, error) {
	var doc kubeconfigDoc
	if err := yaml.NewDecoder(bytes.NewReader(raw)).Decode(&doc); err != nil {
		return "", fmt.Errorf("parse kubeconfig: %w", err)
	}
	if err := ValidateContextName(doc.CurrentContext); err != nil {
		return "", err
	}
	for _, c := range doc.Contexts {
		if c.Name == doc.CurrentContext {
			return doc.CurrentContext, nil
		}
	}
	return "", fmt.Errorf("kubeconfig current-context %q has no matching context", doc.CurrentContext)
}
