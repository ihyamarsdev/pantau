package vault

import "strings"

// InjectEnv returns a NEW map containing all entries of base plus every
// secret injected as prefix+strings.ToUpper(key). The base map is never
// mutated. When prefix is empty the uppercased secret keys are used as-is.
func InjectEnv(base, secrets map[string]string, prefix string) map[string]string {
	out := make(map[string]string, len(base)+len(secrets))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range secrets {
		out[prefix+strings.ToUpper(k)] = v
	}
	return out
}
