package inspector

import (
	"encoding/base64"
	"strings"
)

const KubeDiffOutputLimit = 256 * 1024 // 256 KB

func kubeDiffScript(b64 string) string {
	return `TMP=$(mktemp) || exit 2
printf '%s' ` + shellQuote(b64) + ` | (base64 -d 2>/dev/null || base64 -D) > "$TMP" || { rm -f "$TMP"; exit 2; }
if ! out=$(kc apply --dry-run=server -f "$TMP" 2>&1); then
  echo "Dry-run failed:"
  echo "$out"
  rm -f "$TMP"
  exit 2
fi
kc diff -f "$TMP" 2>&1
rc=$?
rm -f "$TMP"
exit $rc
`
}

func parseKubeDiff(stdout, stderr string, exitCode int) (string, bool, error) {
	out := stdout
	if strings.TrimSpace(out) == "" && exitCode > 1 {
		out = stderr
	}
	if len(out) > KubeDiffOutputLimit {
		out = out[:KubeDiffOutputLimit]
	}

	switch exitCode {
	case 0:
		return out, false, nil
	case 1:
		return out, true, nil
	default:
		return "", false, kubeCommandError("kube diff", stdout, stderr, exitCode)
	}
}

// KubeDiff performs a server dry-run validation followed by kubectl diff.
// Exit 0 = no differences (changed = false, err = nil).
// Exit 1 = differences found (changed = true, err = nil).
// Exit >1 = error (changed = false, err = non-nil).
// Output is truncated to 256 KB.
func (ins *Inspector) KubeDiff(hostID int64, yaml string) (string, bool, error) {
	if err := validateKubeApply(yaml); err != nil {
		return "", false, err
	}
	b64 := base64.StdEncoding.EncodeToString([]byte(yaml))
	script := kubeDiffScript(b64)

	stdout, stderr, exitCode, err := ins.kubeExec(hostID, script, kubeOpTimeout)
	if err != nil {
		return "", false, err
	}
	return parseKubeDiff(stdout, stderr, exitCode)
}
