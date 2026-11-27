package web

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

const tfRunnerJS = `
const fs = require('node:fs'), vm = require('node:vm');
const sandbox = {
  window: {},
  document: {
    addEventListener() {},
    getElementById() { return null; },
    querySelector() { return null; },
    querySelectorAll() { return []; },
    hidden: false,
  },
  I18N: { en: {}, id: {} },
  console,
  setInterval() { return 0; },
  clearInterval() {},
  setTimeout,
  clearTimeout,
  localStorage: { getItem() { return null; }, setItem() {} },
  fetch() { return Promise.resolve({ ok: true, status: 200, text: () => Promise.resolve('{}') }); },
  navigator: {},
  Blob: function () {},
  FormData: function () {},
};
sandbox.window = sandbox;
vm.createContext(sandbox);
vm.runInContext(fs.readFileSync(process.argv[2], 'utf8'), sandbox);

const Tf = sandbox.window.Tf;
process.stdout.write(JSON.stringify({
  stripped: Tf.runs.stripAnsi('\u001b[1;31mError:\u001b[0m \u001b[32mok\u001b[0m'),
  mdSafe: Tf.registry.md('# Heading\n<script>alert(1)</script>\n**Bold** [bad](javascript:alert(1)) [ok](https://example.com/doc)'),
  masked: Tf.stateTab.mask({ access_token: 'secret-tok', password: 'pw', region: 'us-east-1', nested: { client_key: 'k' } }),
}));
`

func TestTerraformBundleHelpers(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is required to evaluate the bundle")
	}
	dir := t.TempDir()
	bundle := filepath.Join(dir, "terraform.js")
	script := filepath.Join(dir, "run.js")
	if err := os.WriteFile(bundle, []byte(terraformJSBundle(t)), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(script, []byte(tfRunnerJS), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(node, script, bundle).CombinedOutput()
	if err != nil {
		t.Fatalf("node failed: %v\n%s", err, out)
	}
	var got struct {
		Stripped string         `json:"stripped"`
		MdSafe   string         `json:"mdSafe"`
		Masked   map[string]any `json:"masked"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("bad json output: %v\n%s", err, out)
	}
	if got.Stripped != "Error: ok" {
		t.Errorf("stripAnsi = %q, want %q", got.Stripped, "Error: ok")
	}
	if containsAny(got.MdSafe, "<script>", "javascript:alert") {
		t.Errorf("registry.md did not sanitize unsafe markup/links: %q", got.MdSafe)
	}
	if !containsAll(got.MdSafe, "<strong>Bold</strong>", `href="https://example.com/doc"`, `rel="noopener noreferrer"`) {
		t.Errorf("registry.md missing expected safe output: %q", got.MdSafe)
	}
	if got.Masked["access_token"] != "(sensitive)" || got.Masked["password"] != "(sensitive)" || got.Masked["region"] != "us-east-1" {
		t.Errorf("stateTab.mask unexpected top-level result: %+v", got.Masked)
	}
	nested, _ := got.Masked["nested"].(map[string]any)
	if nested == nil || nested["client_key"] != "(sensitive)" {
		t.Errorf("stateTab.mask unexpected nested result: %+v", got.Masked["nested"])
	}
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if len(sub) > 0 && containsStr(s, sub) {
			return true
		}
	}
	return false
}

func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		if !containsStr(s, sub) {
			return false
		}
	}
	return true
}

func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || indexStr(s, sub) >= 0)
}

func indexStr(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
