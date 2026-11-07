package terraform

import (
	"errors"
	"net/url"

	"gopkg.in/yaml.v3"
)

// kubeServerHost parses a kubeconfig (it must declare a cluster) and returns the host of
// the server the given context (default: current-context, else the first cluster) points to.
func kubeServerHost(kubeconfig, ctxName string) (string, error) {
	var kc struct {
		CurrentContext string `yaml:"current-context"`
		Clusters       []struct {
			Name    string `yaml:"name"`
			Cluster struct {
				Server string `yaml:"server"`
			} `yaml:"cluster"`
		} `yaml:"clusters"`
		Contexts []struct {
			Name    string `yaml:"name"`
			Context struct {
				Cluster string `yaml:"cluster"`
			} `yaml:"context"`
		} `yaml:"contexts"`
	}
	if err := yaml.Unmarshal([]byte(kubeconfig), &kc); err != nil {
		return "", errors.New("kubeconfig is not valid YAML")
	}
	if len(kc.Clusters) == 0 {
		return "", errors.New("kubeconfig has no clusters")
	}
	if ctxName == "" {
		ctxName = kc.CurrentContext
	}
	server := kc.Clusters[0].Cluster.Server
	if ctxName != "" {
		found := false
		for _, c := range kc.Contexts {
			if c.Name != ctxName {
				continue
			}
			found = true
			for _, cl := range kc.Clusters {
				if cl.Name == c.Context.Cluster {
					server = cl.Cluster.Server
				}
			}
		}
		if !found {
			return "", errors.New("kubeconfig has no context " + ctxName)
		}
	}
	if u, err := url.Parse(server); err == nil && u.Host != "" {
		return u.Host, nil
	}
	return server, nil
}
