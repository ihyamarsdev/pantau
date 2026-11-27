package ansible

import "testing"

// FuzzParseRecap feeds arbitrary playbook logs through the recap parser. It must not panic, and every
// counter it reports must be non-negative even when the digits overflow.
func FuzzParseRecap(f *testing.F) {
	f.Add("PLAY RECAP *****\nweb-1 : ok=3 changed=1 unreachable=0 failed=0 skipped=0 rescued=0 ignored=0\n")
	f.Add("\x1b[0;32mPLAY RECAP\x1b[0m\nhost : ok=99999999999999999999999999 failed=1")
	f.Add("PLAY RECAP\n:\n a : ok= \n")
	f.Add("")
	f.Fuzz(func(t *testing.T, log string) {
		for alias, h := range ParseRecap(log) {
			for name, n := range map[string]int{
				"ok": h.OK, "changed": h.Changed, "unreachable": h.Unreachable, "failed": h.Failed,
				"skipped": h.Skipped, "rescued": h.Rescued, "ignored": h.Ignored,
			} {
				if n < 0 {
					t.Fatalf("host %q counter %s is negative: %d", alias, name, n)
				}
			}
		}
	})
}
