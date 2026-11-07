package terraform

import "testing"

func TestConstraintSatisfied(t *testing.T) {
	tests := []struct {
		c, v    string
		want    bool
		wantErr bool
	}{
		{"", "1.2.3", true, false},
		{"1.2.3", "1.2.3", true, false},
		{"= 1.2.3", "1.2.4", false, false},
		{"=1.2", "1.2.0", true, false},
		{"!= 1.2.3", "1.2.3", false, false},
		{"!= 1.2.3", "1.2.4", true, false},
		{"> 1.2.3", "1.2.4", true, false},
		{"> 1.2.3", "1.2.3", false, false},
		{">= 1.2.3", "1.2.3", true, false},
		{"< 2.0", "1.9.9", true, false},
		{"< 2.0", "2.0.0", false, false},
		{"<= 2.0", "2.0.0", true, false},
		{"~> 5.0", "5.9.1", true, false},
		{"~> 5.0", "6.0.0", false, false},
		{"~> 5.0", "4.9.9", false, false},
		{"~> 5.1.2", "5.1.9", true, false},
		{"~> 5.1.2", "5.2.0", false, false},
		{"~> 5.1.2", "5.1.1", false, false},
		{"~> 0.12", "0.99.0", true, false},
		{"~> 0.12", "1.0.0", false, false},
		{"~> 1", "1.9.0", true, false},
		{"~> 1", "2.0.0", false, false},
		{">= 1.0, < 2.0", "1.5.0", true, false},
		{">= 1.0, < 2.0", "2.0.0", false, false},
		{">= 1.0 , != 1.5.0", "1.5.0", false, false},
		{"~> 5.0, >= 5.3", "5.2.0", false, false},
		{"v1.2.3", "1.2.3", true, false},
		{">= 1.0.0", "1.0.0-rc1", false, false},
		{"< 1.0.0", "1.0.0-rc1", true, false},
		{">= 1.2.3-beta", "1.2.3", true, false},
		{">= 1.0, ", "1.0.0", false, true},
		{">= abc", "1.0.0", false, true},
		{">= 1.x", "1.0.0", false, true},
		{"~>", "1.0.0", false, true},
		{">= 1.0", "latest", false, true},
		{">= 1.0.0.0", "1.0.0", false, true},
	}
	for _, tc := range tests {
		got, err := ConstraintSatisfied(tc.c, tc.v)
		if (err != nil) != tc.wantErr || got != tc.want {
			t.Errorf("ConstraintSatisfied(%q, %q) = %v, %v; want %v, err=%v", tc.c, tc.v, got, err, tc.want, tc.wantErr)
		}
	}
}

func TestConstraintCompareVersions(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"1.2.3", "1.2.3", 0},
		{"1.2.3", "1.10.0", -1},
		{"2.0", "1.99.99", 1},
		{"v1.0.0", "1.0.0", 0},
		{"1.0.0+build1", "1.0.0+build2", 0},
		{"1.0.0-alpha", "1.0.0", -1},
		{"1.0.0", "1.0.0-rc.1", 1},
		{"1.0.0-alpha", "1.0.0-alpha.1", -1},
		{"1.0.0-alpha.1", "1.0.0-alpha.beta", -1},
		{"1.0.0-beta.2", "1.0.0-beta.11", -1},
		{"1.0.0-beta", "1.0.0-alpha", 1},
		{"junk", "0.0.1", -1},
		{"0.0.1", "junk", 1},
		{"junk", "junk", 0},
	}
	for _, tc := range tests {
		if got := CompareVersions(tc.a, tc.b); got != tc.want {
			t.Errorf("CompareVersions(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
		if got := CompareVersions(tc.b, tc.a); got != -tc.want {
			t.Errorf("CompareVersions(%q, %q) = %d, want %d", tc.b, tc.a, got, -tc.want)
		}
	}
}
