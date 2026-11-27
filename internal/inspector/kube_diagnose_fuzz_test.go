package inspector

import (
	"encoding/json"
	"testing"
)

// FuzzK8sDiagnosePreParser feeds arbitrary kubectl JSON to the pod pre-parser. Malformed or hostile output must
// never panic, and a successful parse must still serialize to the compact JSON the LLM receives.
//
// Smoke run: go test -run '^$' -fuzz FuzzK8sDiagnosePreParser -fuzztime=5s ./internal/inspector
func FuzzK8sDiagnosePreParser(f *testing.F) {
	pod := `{"metadata":{"name":"api","namespace":"prod"},"spec":{"nodeName":"n1"},` +
		`"status":{"phase":"Running","containerStatuses":[{"name":"api","restartCount":2,` +
		`"lastState":{"terminated":{"exitCode":137,"reason":"OOMKilled"}}}]}}`
	events := `{"items":[{"type":"Warning","reason":"BackOff","message":"Back-off restarting failed container"}]}`
	f.Add(pod, events)
	f.Add(pod, "")
	f.Add(`{"metadata":{}}`, "")
	f.Add("", "")
	f.Add(`[`, `{`)

	f.Fuzz(func(t *testing.T, podJSON, eventsJSON string) {
		diag, err := parseKubePodDiagnosis(podJSON, eventsJSON)
		if err != nil {
			return
		}
		if diag == nil {
			t.Fatal("nil diagnosis without error")
		}
		if _, err := json.Marshal(diag); err != nil {
			t.Fatalf("diagnosis does not serialize: %v", err)
		}
	})
}
