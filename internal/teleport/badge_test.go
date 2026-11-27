package teleport

import "testing"

func TestSuggestEnvBadge(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		cname  string
		labels map[string]string
		want   string
	}{
		{name: "label env prod", cname: "cluster-a", labels: map[string]string{"env": "prod"}, want: "prod"},
		{name: "label environment staging", cname: "cluster-a", labels: map[string]string{"environment": "Staging"}, want: "staging"},
		{name: "name prod beats label dev", cname: "prod-eu", labels: map[string]string{"env": "dev"}, want: "prod"},
		{name: "name token production", cname: "production-eu-1", want: "prod"},
		{name: "name token stg", cname: "app-stg", want: "staging"},
		{name: "name token dev", cname: "sandbox.dev.internal", want: "dev"},
		{name: "several environments pick riskiest", cname: "dev-and-prod", want: "prod"},
		{name: "staging beats dev", cname: "dev-staging", want: "staging"},
		{name: "no environment word", cname: "cluster-eu-1", labels: map[string]string{"region": "eu"}, want: ""},
		{name: "substring is not a token", cname: "reproduction", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := SuggestEnvBadge(tt.cname, tt.labels); got != tt.want {
				t.Fatalf("SuggestEnvBadge(%q) = %q, want %q", tt.cname, got, tt.want)
			}
		})
	}
}
