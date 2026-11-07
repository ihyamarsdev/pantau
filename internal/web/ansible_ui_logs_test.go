package web

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// runnerJS loads the real bundle in a bare vm context (no DOM) and evaluates the pure log helpers of the run viewer.
const runnerJS = `
const fs = require('node:fs'), vm = require('node:vm');
const sandbox = { window: {}, document: { addEventListener() {}, getElementById() { return null; }, hidden: false }, I18N: { en: {}, id: {} }, console,
  setInterval() { return 0; }, clearInterval() {}, setTimeout, clearTimeout, localStorage: { getItem() { return null; }, setItem() {} }, fetch() {}, navigator: {}, Blob: function () {}, FormData: function () {} };
sandbox.window = sandbox;
vm.createContext(sandbox);
vm.runInContext(fs.readFileSync(process.argv[2], 'utf8'), sandbox);
const u = sandbox.Ans.runs.util, log = JSON.parse(fs.readFileSync(process.argv[3], 'utf8'));
process.stdout.write(JSON.stringify({
  esc: u.ansiToHtml('\u001b[0;32mok\u001b[0m <b>&'),
  tag: u.ansiToHtml('<script>alert(1)</script>'),
  strip: u.stripAnsi('\u001b[1;31mred\u001b[0m'),
  tree: u.parseTaskTree(log.failing),
  diff: u.parseDiff(log.diff),
  none: u.parseDiff(log.failing),
  cron: sandbox.Ans.schedules.util.cronNext('*/10 8-18 * * 1-5', 3),
  badCron: sandbox.Ans.schedules.util.cronNext('61 * * * *', 1),
  rmvar: sandbox.Ans.editor.util.removeYamlVar('- hosts: all\n  vars:\n    a: 1\n    b: 2\n  tasks: []\n', 'a'),
}));
`

const failingLog = "\nPLAY [all] ****\n\nTASK [first] ****\n\x1b[0;32mok: [db-01] => {\x1b[0m\n\x1b[0;32m    \"msg\": \"start\"\x1b[0m\n\x1b[0;32m}\x1b[0m\n\n" +
	"TASK [boom] ****\n\x1b[0;31m[ERROR]: Task failed: Module failed: exit 1\x1b[0m\n\x1b[1;31mfatal: [db-01]: FAILED! => {\"msg\": \"non-zero return code\", \"rc\": 1}\x1b[0m\n" +
	"\x1b[1;31mfatal: [db-02]: UNREACHABLE! => {\"msg\": \"no route\", \"unreachable\": true}\x1b[0m\n\nPLAY RECAP ****\ndb-01 : ok=1 failed=1\n"

const diffLog = "\nPLAY [all] ****\n\nTASK [write file] ****\n\x1b[0;31m--- before\x1b[0m\n\x1b[0;32m+++ after: /tmp/x.txt\x1b[0m\n\x1b[0;36m@@ -0,0 +1,2 @@\x1b[0m\n\x1b[0;32m+one\x1b[0m\n\x1b[0;32m+two\x1b[0m\n\n\x1b[0;33mchanged: [web-01]\x1b[0m\n"

func TestAnsibleBundleLogParsers(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is required to evaluate the bundle")
	}
	dir := t.TempDir()
	bundle := filepath.Join(dir, "ansible.js")
	logs := filepath.Join(dir, "logs.json")
	script := filepath.Join(dir, "run.js")
	raw, _ := json.Marshal(map[string]string{"failing": failingLog, "diff": diffLog})
	for p, b := range map[string][]byte{bundle: []byte(ansibleBundle(t)), logs: raw, script: []byte(runnerJS)} {
		if err := os.WriteFile(p, b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	out, err := exec.Command(node, script, bundle, logs).CombinedOutput()
	if err != nil {
		t.Fatalf("node failed: %v\n%s", err, out)
	}
	var got struct {
		Esc, Tag, Strip string
		Tree            []struct {
			Name  string
			Tasks []struct {
				Name  string
				Hosts []struct{ Host, Status, Detail string }
				Notes []string
			}
		}
		Diff []struct {
			File       string
			Adds, Dels int
			Task       string
		}
		None, Cron, BadCron []any
		Rmvar               string
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("bad output: %v\n%s", err, out)
	}

	if got.Esc != `<span class="ans-fg-2">ok</span> &lt;b&gt;&amp;` {
		t.Errorf("ansi colour/escape: %q", got.Esc)
	}
	if got.Tag != "&lt;script&gt;alert(1)&lt;/script&gt;" {
		t.Errorf("raw log text must be HTML-escaped, got %q", got.Tag)
	}
	if got.Strip != "red" {
		t.Errorf("stripAnsi = %q", got.Strip)
	}
	if len(got.Tree) != 1 || len(got.Tree[0].Tasks) != 2 {
		t.Fatalf("expected 1 play with 2 tasks, got %+v", got.Tree)
	}
	boom := got.Tree[0].Tasks[1]
	if len(boom.Hosts) != 2 || boom.Hosts[0].Status != "fatal" || boom.Hosts[1].Status != "unreachable" || len(boom.Notes) == 0 {
		t.Errorf("failing task parsed wrong: %+v", boom)
	}
	if first := got.Tree[0].Tasks[0]; len(first.Hosts) != 1 || first.Hosts[0].Status != "ok" {
		t.Errorf("ok task parsed wrong: %+v", first)
	}
	if len(got.Diff) != 1 || got.Diff[0].File != "/tmp/x.txt" || got.Diff[0].Adds != 2 || got.Diff[0].Dels != 0 || got.Diff[0].Task != "write file" {
		t.Errorf("diff parsed wrong: %+v", got.Diff)
	}
	if len(got.None) != 0 {
		t.Errorf("a run without diff blocks must yield no files, got %v", got.None)
	}
	if len(got.Cron) != 3 || got.BadCron != nil {
		t.Errorf("cron preview: next=%v bad=%v", got.Cron, got.BadCron)
	}
	if got.Rmvar != "- hosts: all\n  vars:\n    b: 2\n  tasks: []\n" {
		t.Errorf("removeYamlVar = %q", got.Rmvar)
	}
}
