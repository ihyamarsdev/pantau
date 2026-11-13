package web

import "testing"

func TestCleanShortcuts(t *testing.T) {
	got, err := cleanShortcuts([]shortcut{{Name: " Grafana ", URL: " https://grafana.example.com:3000/d/1 ", Group: " Monitoring "}})
	if err != nil {
		t.Fatalf("valid shortcut rejected: %v", err)
	}
	want := shortcut{Name: "Grafana", URL: "https://grafana.example.com:3000/d/1", Group: "Monitoring"}
	if len(got) != 1 || got[0] != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}

	// Each of these would be a link that runs script, opens no site, or has no label.
	rejected := []shortcut{
		{Name: "x", URL: "javascript:alert(1)"},
		{Name: "x", URL: "https://javascript:alert(1)"},
		{Name: "x", URL: "data:text/html,<script>alert(1)</script>"},
		{Name: "x", URL: "ftp://files.example.com"},
		{Name: "x", URL: "example.com"},
		{Name: "x", URL: "https://"},
		{Name: "x", URL: ""},
		{Name: " ", URL: "https://example.com"},
	}
	for _, sc := range rejected {
		if _, err := cleanShortcuts([]shortcut{sc}); err == nil {
			t.Errorf("accepted %+v", sc)
		}
	}

	if _, err := cleanShortcuts(make([]shortcut, maxShortcuts+1)); err == nil {
		t.Errorf("accepted more than %d shortcuts", maxShortcuts)
	}
}
