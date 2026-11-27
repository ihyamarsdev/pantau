package terraform

import "testing"

// FuzzParseState feeds arbitrary state files through the parser. It must not panic, and a rejected
// file must not return partial results.
func FuzzParseState(f *testing.F) {
	f.Add([]byte(`{"version":4,"serial":1,"lineage":"abc","outputs":{"o":{"value":"v","type":"string","sensitive":true}},"resources":[]}`))
	f.Add([]byte(`{"version":3}`))
	f.Add([]byte(`{"version":4,"resources":[{"mode":"managed","type":"t","name":"n","instances":[{"attributes":{"password":"p"},"sensitive_attributes":["password"]}]}]}`))
	f.Add([]byte(`not json`))
	f.Fuzz(func(t *testing.T, b []byte) {
		info, err := ParseState(b)
		if err != nil && info != nil {
			t.Fatalf("returned partial state alongside error %v", err)
		}
		if err == nil && info == nil {
			t.Fatal("nil state without error")
		}
	})
}
