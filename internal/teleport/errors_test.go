package teleport

import "testing"

func TestIsSessionExpiredOutput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		out  string
		want bool
	}{
		{name: "x509 certificate expired", out: "Get https://k8s: x509: certificate has expired or is not yet valid", want: true},
		{name: "tsh credentials expired", out: "ERROR: your credentials have expired, please login using `tsh login`", want: true},
		{name: "tsh not logged in", out: "ERROR: Not logged in.", want: true},
		{name: "kube api unauthorized", out: `error: You must be logged in to the server (Unauthorized)`, want: true},
		{name: "ordinary kubectl error", out: `Error from server (NotFound): pods "web" not found`, want: false},
		{name: "forbidden is not an expired session", out: `Error from server (Forbidden): pods is forbidden`, want: false},
		{name: "empty output", out: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := IsSessionExpiredOutput(tt.out); got != tt.want {
				t.Fatalf("IsSessionExpiredOutput(%q) = %v, want %v", tt.out, got, tt.want)
			}
		})
	}
}
