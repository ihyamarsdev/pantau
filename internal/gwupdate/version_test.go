package gwupdate

import "testing"

func TestParseVersion(t *testing.T) {
	cases := []struct {
		in   string
		ok   bool
		core string
	}{
		{"1.2.3", true, "1.2.3"},
		{"v1.15.9", true, "1.15.9"},
		{"V1.2", true, "1.2"},
		{"1", true, "1"},
		{"1.0.0-rc1", true, "1.0.0"},
		{"1.0.0-rc1+build.5", true, "1.0.0"},
		{"dev", false, ""},
		{"1.2.3.4", false, ""},
		{"1..3", false, ""},
		{"1.-2", false, ""},
		{"1.0.0-", false, ""},
		{"latest", false, ""},
	}
	for _, c := range cases {
		v, ok := ParseVersion(c.in)
		if ok != c.ok {
			t.Errorf("ParseVersion(%q) ok = %v, want %v", c.in, ok, c.ok)
			continue
		}
		if !ok {
			continue
		}
		got := ""
		for i, n := range v.core {
			if i > 0 {
				got += "."
			}
			got += itoa(n)
		}
		if got != c.core {
			t.Errorf("ParseVersion(%q) core = %q, want %q", c.in, got, c.core)
		}
	}
}

func TestCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.2.3", "1.2.3", 0},
		{"1.2", "1.2.0", 0},
		{"v1.2.3", "1.2.3", 0},
		{"1.17.2", "1.15.9", 1},
		{"1.15.9", "1.17.2", -1},
		{"2.0.0", "1.99.99", 1},
		{"1.0.0", "1.0.0-rc1", 1},
		{"1.0.0-rc1", "1.0.0", -1},
		{"1.0.0-rc1", "1.0.0-rc2", -1},
		{"1.0.0-rc2", "1.0.0-rc1", 1},
		{"1.0.0-rc1", "1.0.0-rc1", 0},
		// An unparsable version sorts below any parsable one, and never above it.
		{"dev", "1.0.0", -1},
		{"1.0.0", "dev", 1},
		{"dev", "dev", 0},
	}
	for _, c := range cases {
		if got := Compare(c.a, c.b); got != c.want {
			t.Errorf("Compare(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestExtractVersion(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		// The real output of the three CLIs behind the Pantau gateways.
		{"ansible-playbook [core 2.22.0]", "2.22.0"},
		{"ansible-playbook [core 2.22.0]\n  config file = None\n", "2.22.0"},
		{"Terraform v1.15.9\non linux_amd64", "1.15.9"},
		{"Terraform v1.15.9", "1.15.9"},
		{"OpenTofu v1.13.9", "1.13.9"},
		// Other shapes a version line may take.
		{"ansible-playbook 2.18.0", "2.18.0"},
		{"tofu v1.9.0", "1.9.0"},
		{"Terraform v1.15.9-rc1", "1.15.9-rc1"},
		{"terraform 1.6.0", "1.6.0"},
		// Lines that hold no version at all.
		{"python3.11", ""},
		{"", ""},
		{"command not found", ""},
		{"Terraform", ""},
	}
	for _, c := range cases {
		if got := ExtractVersion(c.in); got != c.want {
			t.Errorf("ExtractVersion(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// The version a CLI reports must compare as newer or equal to itself, whatever the shape of the line.
func TestExtractVersionRoundTrip(t *testing.T) {
	lines := []string{
		"ansible-playbook [core 2.22.0]",
		"Terraform v1.15.9",
		"OpenTofu v1.13.9",
	}
	for _, line := range lines {
		v := ExtractVersion(line)
		if v == "" {
			t.Fatalf("ExtractVersion(%q) found no version", line)
		}
		if Compare(v, v) != 0 {
			t.Errorf("Compare(%q, %q) != 0", v, v)
		}
	}
}
