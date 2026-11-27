package terraform

import (
	"encoding/json"
	"testing"
)

// FuzzTerraformPlanPreParser feeds arbitrary `terraform show -json` payloads to the plan pre-parser. Hostile or
// truncated plans must never panic, and a successful summary must still serialize.
//
// Smoke run: go test -run '^$' -fuzz FuzzTerraformPlanPreParser -fuzztime=5s ./internal/terraform
func FuzzTerraformPlanPreParser(f *testing.F) {
	f.Add([]byte(`{"resource_changes":[{"address":"aws_instance.web","type":"aws_instance","change":{"actions":["create"],"after":{"user_data":"s3cret"},"after_sensitive":{"user_data":true}}}]}`))
	f.Add([]byte(`{"resource_changes":[{"address":"x.y","change":{"actions":["delete","create"]}}]}`))
	f.Add([]byte(`{"output_changes":{"o":{"actions":["update"]}}}`))
	f.Add([]byte(`{}`))
	f.Add([]byte(`[`))

	f.Fuzz(func(t *testing.T, planJSON []byte) {
		analysis, err := AnalyzePlanJSON(planJSON)
		if err != nil {
			return
		}
		if analysis == nil {
			t.Fatal("nil analysis without error")
		}
		if _, err := json.Marshal(analysis); err != nil {
			t.Fatalf("analysis does not serialize: %v", err)
		}
	})
}
