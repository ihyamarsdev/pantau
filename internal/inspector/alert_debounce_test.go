package inspector

import "testing"

func TestAlertDebouncer_StateMachine(t *testing.T) {
	tests := []struct {
		name     string
		runs     int
		steps    []string // series of status observations: "drift", "ok", etc.
		expected []bool   // whether each step should trigger an alert
	}{
		{
			name:     "debounce runs 1 triggers immediately on first drift, not repeated drifts",
			runs:     1,
			steps:    []string{"drift", "drift", "ok", "drift"},
			expected: []bool{true, false, false, true},
		},
		{
			name:     "debounce runs 3 triggers only on 3rd consecutive drift",
			runs:     3,
			steps:    []string{"drift", "drift", "ok", "drift", "drift", "drift", "drift"},
			expected: []bool{false, false, false, false, false, true, false},
		},
		{
			name:     "healthy reset immediately clears drift counter",
			runs:     2,
			steps:    []string{"drift", "ok", "drift", "ok", "drift", "drift"},
			expected: []bool{false, false, false, false, false, true},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			debouncer := NewAlertDebouncer()
			hostID := int64(10)
			kind := "service"
			target := "nginx"

			for i, status := range tc.steps {
				got := debouncer.Observe(hostID, kind, target, status, tc.runs)
				if got != tc.expected[i] {
					t.Fatalf("step %d (%s): expected alert=%v, got %v", i, status, tc.expected[i], got)
				}
			}
		})
	}
}

func TestParseDebounceRuns(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"", 1},
		{"0", 1},
		{"-5", 1},
		{"invalid", 1},
		{"1", 1},
		{"3", 3},
		{"10", 10},
		{"11", 10},
		{"99", 10},
		{" 5 \n", 5},
	}
	for _, c := range cases {
		got := ParseDebounceRuns(c.in)
		if got != c.want {
			t.Errorf("ParseDebounceRuns(%q) = %d; want %d", c.in, got, c.want)
		}
	}
}
