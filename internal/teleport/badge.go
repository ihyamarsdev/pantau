package teleport

import "strings"

// envWords maps a word found in a label value or in a cluster name to an environment badge.
var envWords = map[string]string{
	"prod": "prod", "production": "prod",
	"staging": "staging", "stage": "staging", "stg": "staging",
	"dev": "dev", "development": "dev",
}

// envLabelKeys are the label keys that name an environment.
var envLabelKeys = []string{"env", "environment", "stage"}

// SuggestEnvBadge proposes prod, staging or dev from the labels first, then from the cluster name. When the
// name carries several environments it picks the riskiest one (prod before staging before dev). It returns ""
// when nothing matches, and the operator sets the badge by hand.
func SuggestEnvBadge(name string, labels map[string]string) string {
	found := map[string]bool{}
	for _, key := range envLabelKeys {
		if v, ok := labels[key]; ok {
			for _, word := range splitWords(v) {
				if env, ok := envWords[word]; ok {
					found[env] = true
				}
			}
		}
	}
	for _, word := range splitWords(name) {
		if env, ok := envWords[word]; ok {
			found[env] = true
		}
	}
	for _, env := range []string{"prod", "staging", "dev"} {
		if found[env] {
			return env
		}
	}
	return ""
}

// splitWords lower-cases s and splits it on anything that is not a letter or digit.
func splitWords(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9')
	})
}
