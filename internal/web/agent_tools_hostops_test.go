package web

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pkg/sftp"
	"pantau/internal/inspector"
	"pantau/internal/sshrunner"
	"pantau/internal/store"
	"pantau/internal/transfer"
)

// hostOpsServer builds a server whose inspector talks to a mock SSH runner, plus one host.
func hostOpsServer(t *testing.T) (*Server, *store.DB, *sshrunner.MockRunner, int64) {
	t.Helper()
	runner := sshrunner.NewMockRunner()
	srv, db, _ := setupDiskUsageServer(t, runner)
	_ = db.SetSetting("agent_enabled", "1")
	hostID, err := db.CreateHost(&store.Host{Name: "web-1", Host: "10.0.0.1", Port: 22, User: "root"})
	if err != nil {
		t.Fatal(err)
	}
	return srv, db, runner, hostID
}

// newSFTPRunnerInspector builds an inspector whose SFTP sessions serve a local directory, so the File
// Manager handlers run against real files.
func newSFTPRunnerInspector(t *testing.T, db *store.DB, runner *sshrunner.MockRunner, dir string) *inspector.Inspector {
	t.Helper()
	runner.SFTPProvider = func() (*sftp.Client, error) { return sftpOverPipe() }
	return inspector.New(db, func(sshrunner.Target) (sshrunner.Runner, error) { return runner, nil }, nil)
}

// copyFiles is the mock's answer to `cp -a src dst`: it copies the last file of the command into the
// directory of the last argument, so the duplicate handler sees a real copy.
func copyFiles(t *testing.T, runner *sshrunner.MockRunner) string {
	t.Helper()
	var cmd string
	for _, c := range runner.ExecHistory {
		if strings.Contains(c, "cp -a") {
			cmd = c
		}
	}
	fields := strings.Fields(cmd)
	// The command is `cp -a -- <src> <dst>`: the two paths are the last two fields.
	var src, dst string
	if len(fields) >= 2 {
		src, dst = fields[len(fields)-2], fields[len(fields)-1]
	}
	if src == "" || dst == "" {
		return ""
	}
	src = strings.Trim(src, "'")
	dst = strings.Trim(dst, "'")
	if err := copyPath(src, dst); err != nil {
		return err.Error()
	}
	return ""
}

// copyPath copies a file or a directory tree.
func copyPath(src, dst string) error {
	st, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !st.IsDir() {
		b, err := os.ReadFile(src)
		if err != nil {
			return err
		}
		return os.WriteFile(dst, b, st.Mode().Perm())
	}
	return filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode().Perm())
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, b, info.Mode().Perm())
	})
}

// The host runtime tools must be registered, with an object schema and a description.
func TestAgentHostOpsToolsListed(t *testing.T) {
	want := []string{"docker_container_action", "docker_run", "docker_image_action", "docker_volume_action",
		"docker_network_action", "docker_prune", "docker_stack_deploy", "pkg_action", "file_action",
		"file_upload_text", "transfer_action", "transfer_jobs", "domain_config_remove"}
	have := map[string]agentTool{}
	for _, tl := range agentTools {
		if _, dup := have[tl.Name]; dup {
			t.Fatalf("duplicate tool %s", tl.Name)
		}
		have[tl.Name] = tl
	}
	for _, n := range want {
		tl, ok := have[n]
		if !ok || tl.run == nil {
			t.Fatalf("tool %s missing", n)
		}
		if tl.InputSchema["type"] != "object" || tl.Description == "" {
			t.Fatalf("tool %s has no object schema or no description", n)
		}
		b, _ := json.Marshal(tl.InputSchema)
		if strings.Contains(string(b), `"required":null`) {
			t.Fatalf("%s: schema has required:null: %s", n, b)
		}
	}
}

// A wrong argument is an error and changes nothing: no approval card, no SSH command.
func TestAgentHostOpsValidation(t *testing.T) {
	srv, db, runner, hostID := hostOpsServer(t)
	defer db.Close()
	_ = db.SetSetting("agent_approval", "auto") // a gate-1 action must not block the test
	h := itoa(hostID)

	for _, c := range []struct{ tool, args string }{
		{"docker_container_action", `{"host_id":` + h + `,"container":"web","action":"exec"}`},
		{"docker_container_action", `{"host_id":` + h + `,"container":"web","action":"rename"}`},
		{"docker_container_action", `{"host_id":` + h + `,"container":"bad name","action":"start"}`},
		{"docker_run", `{"host_id":` + h + `,"image":"not a valid image"}`},
		{"docker_image_action", `{"host_id":` + h + `,"action":"push"}`},
		{"docker_image_action", `{"host_id":` + h + `,"action":"remove"}`},
		{"docker_volume_action", `{"host_id":` + h + `,"action":"remove"}`},
		{"docker_network_action", `{"host_id":` + h + `,"action":"remove"}`},
		{"docker_prune", `{"host_id":` + h + `,"target":"everything"}`},
		{"pkg_action", `{"host_id":` + h + `,"action":"reinstall"}`},
		{"file_action", `{"host_id":` + h + `,"action":"write","path":"relative/path"}`},
		{"file_action", `{"host_id":` + h + `,"action":"duplicate","path":"/srv/a"}`},
		{"file_action", `{"host_id":` + h + `,"action":"chmod","path":"/srv/a"}`},
		{"file_upload_text", `{"host_id":` + h + `,"path":"/srv/a"}`},
		{"transfer_action", `{"host_id":` + h + `,"action":"transfer","path":"/srv/a"}`},
		{"transfer_action", `{"host_id":` + h + `,"action":"s3","path":"/srv/a"}`},
		{"transfer_action", `{"host_id":` + h + `,"action":"cancel"}`},
		{"domain_config_remove", `{"host_id":` + h + `,"path":"nginx.conf"}`},
	} {
		out, isErr := callTool(t, srv, c.tool, c.args)
		if !isErr {
			t.Errorf("%s %s must be an error: %s", c.tool, c.args, out)
		}
	}
	if pendingCount(srv) != 0 {
		t.Fatalf("a refused action must not ask for approval (%d pending)", pendingCount(srv))
	}
	if len(runner.ExecHistory) != 0 {
		t.Fatalf("a refused action must not touch the host: %v", runner.ExecHistory)
	}
}

// Every destructive action waits for the operator, even in auto mode, and a denial runs nothing.
func TestAgentHostOpsDestructiveActionsAsk(t *testing.T) {
	srv, db, runner, hostID := hostOpsServer(t)
	defer db.Close()
	_ = db.SetSetting("agent_approval", "auto") // auto skips gate 1 only
	h := itoa(hostID)

	for _, c := range []struct{ tool, args, want string }{
		{"docker_container_action", `{"host_id":` + h + `,"container":"web","action":"kill"}`, "docker kill web"},
		{"docker_container_action", `{"host_id":` + h + `,"container":"web","action":"remove"}`, "docker remove web"},
		{"docker_prune", `{"host_id":` + h + `,"target":"volumes"}`, "docker prune volumes"},
		{"docker_image_action", `{"host_id":` + h + `,"action":"prune"}`, "docker image prune"},
		{"docker_volume_action", `{"host_id":` + h + `,"action":"remove","name":"data"}`, "docker volume remove data"},
		{"docker_network_action", `{"host_id":` + h + `,"action":"remove","name":"app-net"}`, "docker network remove app-net"},
	} {
		card := make(chan string, 1)
		go func() { card <- decideNext(t, srv, false) }()
		out, _ := callTool(t, srv, c.tool, c.args)
		if !strings.Contains(out, "denied") {
			t.Errorf("%s: a denied action must report the denial: %s", c.tool, out)
		}
		if got := <-card; !strings.Contains(got, c.want) {
			t.Errorf("%s: approval card must show the action, want %q: %s", c.tool, c.want, got)
		}
	}
	if pendingCount(srv) != 0 {
		t.Fatalf("pending approvals left: %d", pendingCount(srv))
	}
	if len(runner.ExecHistory) != 0 {
		t.Fatalf("a denied action must not touch the host: %v", runner.ExecHistory)
	}
}

// The File Manager writes through the UI handler, so a protected path is refused (and nothing is asked).
func TestAgentHostOpsFileWriteRefusesProtectedPath(t *testing.T) {
	srv, db, _, hostID := hostOpsServer(t)
	defer db.Close()
	h := itoa(hostID)

	for _, p := range []string{"/etc/passwd", "/usr/bin/ls", "/root/.bashrc", "/"} {
		out, isErr := callTool(t, srv, "file_action", `{"host_id":`+h+`,"action":"write","path":"`+p+`","content":"x"}`)
		if !isErr || !strings.Contains(out, "protected path") {
			t.Errorf("writing %s must be refused: %s", p, out)
		}
	}
	if pendingCount(srv) != 0 {
		t.Fatalf("a refused write must not ask for approval (%d pending)", pendingCount(srv))
	}
}

// The File Manager write, duplicate, delete and chmod run through the real handler: the file really changes.
func TestAgentHostOpsFileActions(t *testing.T) {
	srv, db, runner, hostID := hostOpsServer(t)
	defer db.Close()
	_ = db.SetSetting("agent_approval", "auto")
	h := itoa(hostID)

	dir := t.TempDir()
	srv.inspector = newSFTPRunnerInspector(t, db, runner, dir)

	// The mock runner does not really copy: the duplicate handler runs cp -a on the host.
	runner.Handlers["cp -a"] = func() (string, string, int, error) {
		return copyFiles(t, runner), "", 0, nil
	}
	target := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(target, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	// write: the content of the file is the new content.
	go func() { decideNext(t, srv, true) }()
	out, isErr := callTool(t, srv, "file_action", `{"host_id":`+h+`,"action":"write","path":"`+target+`","content":"hello"}`)
	if isErr || !strings.Contains(out, "saved") {
		t.Fatalf("write: %s", out)
	}
	if b, _ := os.ReadFile(target); string(b) != "hello" {
		t.Fatalf("the file was not written: %q", b)
	}

	// duplicate: the copy sits next to the original (a routine change: no card in auto mode).
	out, isErr = callTool(t, srv, "file_action", `{"host_id":`+h+`,"action":"duplicate","path":"`+target+`","new_name":"copy.txt"}`)
	if isErr || !strings.Contains(out, "copy.txt") {
		t.Fatalf("duplicate: %s", out)
	}
	if _, err := os.Stat(filepath.Join(dir, "copy.txt")); err != nil {
		t.Fatalf("the copy is missing: %v", err)
	}

	// chmod: the mode changes.
	go func() { decideNext(t, srv, true) }()
	out, isErr = callTool(t, srv, "file_action", `{"host_id":`+h+`,"action":"chmod","path":"`+target+`","mode":"0600"}`)
	if isErr || !strings.Contains(out, "chmod_ok") {
		t.Fatalf("chmod: %s", out)
	}
	st, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, want 0600", st.Mode().Perm())
	}

	// delete: the file is gone.
	go func() { decideNext(t, srv, true) }()
	out, isErr = callTool(t, srv, "file_action", `{"host_id":`+h+`,"action":"delete","path":"`+target+`"}`)
	if isErr || !strings.Contains(out, "deleted") {
		t.Fatalf("delete: %s", out)
	}
	if _, err := os.Stat(target); err == nil {
		t.Fatal("the file is still there after the delete")
	}
}

// The Docker container actions reach the host and the destructive ones ask first.
func TestAgentHostOpsDockerContainerAction(t *testing.T) {
	srv, db, runner, hostID := hostOpsServer(t)
	defer db.Close()
	_ = db.SetSetting("agent_approval", "auto")
	h := itoa(hostID)

	// start is a routine change: it runs without a card.
	out, isErr := callTool(t, srv, "docker_container_action", `{"host_id":`+h+`,"container":"web","action":"start"}`)
	if isErr || !strings.Contains(out, "docker start web") {
		t.Fatalf("start: %s", out)
	}
	if !ranCommand(runner, "docker start 'web'") {
		t.Fatalf("the command did not reach the host: %v", runner.ExecHistory)
	}

	// remove asks, and the approval shows the action.
	card := make(chan string, 1)
	go func() { card <- decideNext(t, srv, true) }()
	out, isErr = callTool(t, srv, "docker_container_action", `{"host_id":`+h+`,"container":"web","action":"remove","force":true}`)
	if isErr || !strings.Contains(out, "docker remove web") {
		t.Fatalf("remove: %s", out)
	}
	if c := <-card; !strings.Contains(c, "docker remove web") {
		t.Fatalf("approval card: %s", c)
	}
	if !ranCommand(runner, "docker rm -f 'web'") {
		t.Fatalf("the remove did not reach the host: %v", runner.ExecHistory)
	}
}

// The S3 upload names an account the operator stored: the secret key never crosses the model.
func TestAgentHostOpsS3Upload(t *testing.T) {
	srv, db, _, hostID := hostOpsServer(t)
	defer db.Close()
	_ = db.SetSetting("agent_approval", "auto")
	h := itoa(hostID)
	if _, err := db.CreateS3Account(&store.S3Account{Name: "backup", Bucket: "pantau-backups", AccessKey: "AKIAEXAMPLE", SecretKey: "super-secret-key"}); err != nil {
		t.Fatal(err)
	}

	// An unknown account is refused before anything runs.
	if out, isErr := callTool(t, srv, "transfer_action", `{"host_id":`+h+`,"action":"s3","path":"/srv/a.txt","s3_account":"nope"}`); !isErr || !strings.Contains(out, "no S3 account") {
		t.Fatalf("unknown account: %s", out)
	}
	if pendingCount(srv) != 0 {
		t.Fatalf("a refused upload must not ask (%d pending)", pendingCount(srv))
	}

	go func() { decideNext(t, srv, true) }()
	out, isErr := callTool(t, srv, "transfer_action", `{"host_id":`+h+`,"action":"s3","path":"/srv/a.txt","s3_account":"backup"}`)
	if isErr {
		t.Fatalf("s3 upload: %s", out)
	}
	if strings.Contains(out, "super-secret-key") || strings.Contains(out, "AKIAEXAMPLE") {
		t.Fatalf("the credentials must not appear in the result: %s", out)
	}
}

// The package change asks first and never carries a secret.
func TestAgentHostOpsPkgAction(t *testing.T) {
	srv, db, runner, hostID := hostOpsServer(t)
	defer db.Close()
	h := itoa(hostID)

	// The preview says the host has no supported package manager: nothing runs.
	runner.Handlers["MGR:"] = func() (string, string, int, error) {
		return "===OS_RELEASE===\n===REBOOT===\n===UPDATES===\nMGR:unknown\n", "", 0, nil
	}
	out, isErr := callTool(t, srv, "pkg_action", `{"host_id":`+h+`,"action":"upgrade"}`)
	if !isErr || !strings.Contains(out, "not supported") {
		t.Fatalf("upgrade on an unsupported host: %s", out)
	}
	if pendingCount(srv) != 0 {
		t.Fatalf("a refused upgrade must not ask (%d pending)", pendingCount(srv))
	}

	// A supported host: the approval card shows the command and the run reaches the host.
	runner.Handlers["MGR:"] = func() (string, string, int, error) {
		return "===OS_RELEASE===\nNAME=\"Debian\"\n===REBOOT===\n===UPDATES===\nMGR:apt\nInst nginx [1.20.0] (1.22.0 Debian-Security)\n", "", 0, nil
	}
	card := make(chan string, 1)
	go func() { card <- decideNext(t, srv, false) }()
	out, _ = callTool(t, srv, "pkg_action", `{"host_id":`+h+`,"action":"upgrade"}`)
	if !strings.Contains(out, "denied") {
		t.Fatalf("a denied upgrade must report the denial: %s", out)
	}
	if c := <-card; !strings.Contains(c, "apt-get") {
		t.Fatalf("the approval card must show the package command: %s", c)
	}
}

// ranCommand reports whether one of the commands the runner saw contains the fragment. The commands are
// shell-quoted, so quotes and the backslashes of quoted quotes are ignored.
func ranCommand(runner *sshrunner.MockRunner, fragment string) bool {
	strip := strings.NewReplacer("'", "", "\\", "")
	want := strip.Replace(fragment)
	for _, c := range runner.ExecHistory {
		if strings.Contains(strip.Replace(c), want) {
			return true
		}
	}
	return false
}

// The transfer jobs are readable and a new transfer is a job the operator can follow.
func TestAgentHostOpsTransferJobs(t *testing.T) {
	srv, db, runner, hostID := hostOpsServer(t)
	defer db.Close()
	_ = db.SetSetting("agent_approval", "auto")
	h := itoa(hostID)
	srv.SetTransferManager(transfer.NewManager(db, func(sshrunner.Target) (sshrunner.Runner, error) { return runner, nil }))
	destID, err := db.CreateHost(&store.Host{Name: "web-2", Host: "10.0.0.2", Port: 22, User: "root"})
	if err != nil {
		t.Fatal(err)
	}

	// A transfer to another host starts a job that the operator can follow.
	go func() { decideNext(t, srv, true) }()
	out, isErr := callTool(t, srv, "transfer_action", `{"host_id":`+h+`,"action":"transfer","path":"/srv/a.txt","dest_host_id":`+itoa(destID)+`,"dest_path":"/srv/b.txt"}`)
	if isErr || !strings.Contains(out, "job") {
		t.Fatalf("transfer: %s", out)
	}
	jobs, isErr := callTool(t, srv, "transfer_jobs", `{}`)
	if isErr {
		t.Fatalf("transfer_jobs: %s", jobs)
	}
	if !strings.Contains(jobs, "/srv/a.txt") {
		t.Fatalf("the job must be listed: %s", jobs)
	}
}

// The host runtime tools must be mentioned in the SOUL, or the agent never uses them.
func TestAgentHostOpsSoulMentionsTools(t *testing.T) {
	for _, n := range []string{"docker_container_action", "docker_run", "docker_image_action", "docker_volume_action",
		"docker_network_action", "docker_prune", "docker_stack_deploy", "pkg_action", "file_action",
		"file_upload_text", "transfer_action", "transfer_jobs", "domain_config_remove"} {
		if !strings.Contains(defaultAgentSoul, "`"+n+"`") {
			t.Errorf("agent_soul.md does not mention tool %s", n)
		}
	}
}

// The package change must not run longer than the tool timeout allows.
func TestAgentHostOpsPkgTimeoutIsBounded(t *testing.T) {
	if agentExecTimeout > 15*time.Minute {
		t.Fatalf("the package tool timeout (%v) must stay inside the agent tool timeout (%v)", 15*time.Minute, agentExecTimeout)
	}
}
