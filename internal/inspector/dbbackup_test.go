package inspector

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseDBScan(t *testing.T) {
	out := `BIN mysqldump
BIN pg_dumpall
PROC mysqld
PROC postgres
PROC mongod
CTR shop-db|postgres:16
CTR cache|redis:7
CTR pg-exporter|prometheuscommunity/postgres-exporter:latest
CTR adminer|adminer:4
CTR blog-db|docker.io/library/mariadb:11
`
	got := parseDBScan(out)
	var summary []string
	for _, s := range got {
		summary = append(summary, s.Engine+"/"+s.Where+"/"+s.Container+"/"+s.Missing)
	}
	want := []string{"mysql/host//", "postgres/host//", "mongo/host//mongodump", "mysql/container/blog-db/", "postgres/container/shop-db/"}
	if strings.Join(summary, " ") != strings.Join(want, " ") {
		t.Fatalf("got %v, want %v", summary, want)
	}
}

func TestDBDumpCommandRejectsBadInput(t *testing.T) {
	for _, c := range [][3]string{{"oracle", "host", ""}, {"mysql", "elsewhere", ""}, {"mysql", "container", "x; rm -rf /"}, {"mysql", "container", ""}} {
		if _, err := DBDumpCommand(c[0], c[1], c[2]); err == nil {
			t.Errorf("DBDumpCommand%v must fail", c)
		}
	}
}

// The scripts run against fake dump tools: credentials must reach the tool through the
// environment or a config descriptor, never through argv.
func fakeDumpTools(t *testing.T) string {
	dir := t.TempDir()
	fake := "#!/bin/sh\necho \"args=$* MYSQL_PWD=$MYSQL_PWD PGPASSWORD=$PGPASSWORD\"\ncat /dev/fd/3 2>/dev/null\nexit 0\n"
	for _, tool := range []string{"mariadb-dump", "pg_dumpall", "mongodump"} {
		if err := os.WriteFile(filepath.Join(dir, tool), []byte(fake), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func runScript(t *testing.T, dir, script, stdin string, env ...string) string {
	cmd := exec.Command("sh", "-c", script)
	cmd.Env = append([]string{"PATH=" + dir + ":/usr/bin:/bin"}, env...)
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("script failed: %v: %s", err, out)
	}
	return string(out)
}

func TestDBContainerScriptsPassCredentials(t *testing.T) {
	dir := fakeDumpTools(t)
	run := func(engine, stdin string, env ...string) string {
		return runScript(t, dir, dbContainerScripts[engine], stdin, env...)
	}
	if out := run("mysql", "", "MARIADB_ROOT_PASSWORD=s3cret"); !strings.Contains(out, "-uroot --all-databases") || !strings.Contains(out, "MYSQL_PWD=s3cret") || strings.Contains(strings.Split(out, "MYSQL_PWD")[0], "s3cret") {
		t.Errorf("mysql: %s", out)
	}
	if out := run("postgres", "", "POSTGRES_USER=app", "POSTGRES_PASSWORD=pw"); !strings.Contains(out, "args=-U app") || !strings.Contains(out, "PGPASSWORD=pw") {
		t.Errorf("postgres: %s", out)
	}
	if out := run("postgres", ""); !strings.Contains(out, "args=-U postgres") {
		t.Errorf("postgres default user: %s", out)
	}
	// MongoDB: image variables by default, a login on stdin overrides them; the password never is an argument
	out := run("mongo", "", "MONGO_INITDB_ROOT_USERNAME=root", "MONGO_INITDB_ROOT_PASSWORD=pw")
	if !strings.Contains(out, "--username root --authenticationDatabase admin") || !strings.Contains(out, `password: "pw"`) || strings.Contains(strings.Split(out, "password:")[0], "pw") {
		t.Errorf("mongo with env login: %s", out)
	}
	out = run("mongo", "u2\np\"w\\x$y\nauthx\n", "MONGO_INITDB_ROOT_USERNAME=root", "MONGO_INITDB_ROOT_PASSWORD=pw")
	if !strings.Contains(out, "--username u2 --authenticationDatabase authx") || !strings.Contains(out, `password: "p\"w\\x$y"`) {
		t.Errorf("mongo with stdin login (quote, backslash, dollar must survive): %s", out)
	}
	if out := run("mongo", ""); strings.Contains(out, "--username") {
		t.Errorf("mongo without login must not pass credentials: %s", out)
	}
}

// A host mongod that requires TLS is dumped with the TLS files named in mongod.conf.
func TestDBHostMongoScriptReadsMongodConf(t *testing.T) {
	dir := fakeDumpTools(t)
	conf := func(body string) string {
		p := filepath.Join(t.TempDir(), "mongod.conf")
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return strings.ReplaceAll(dbHostScripts["mongo"], "/etc/mongod.conf", p)
	}
	tls := "net:\n  port: 27018\n  bindIp: 127.0.0.1,10.0.4.1\n  tls:\n    mode: requireTLS # comment\n    certificateKeyFile: '/etc/m.pem'\n    CAFile: /etc/ca.crt\n"
	out := runScript(t, dir, conf(tls), "")
	for _, want := range []string{"--ssl", "--sslCAFile /etc/ca.crt", "--sslPEMKeyFile /etc/m.pem", "--port 27018"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in %s", want, out)
		}
	}
	if strings.Contains(out, "--host") {
		t.Errorf("a bindIp that includes 127.0.0.1 needs no --host: %s", out)
	}
	if out := runScript(t, dir, conf("net:\n  bindIp: 10.1.1.1\n"), ""); !strings.Contains(out, "--host 10.1.1.1") || strings.Contains(out, "--ssl") {
		t.Errorf("plain mongod bound to one address: %s", out)
	}
}

func TestDBMongoLogin(t *testing.T) {
	if got, err := DBMongoLogin("", "x", "y"); got != "" || err != nil {
		t.Errorf("empty user means no login, got %q %v", got, err)
	}
	if got, _ := DBMongoLogin("u", "p", "admin"); got != "u\np\nadmin\n" {
		t.Errorf("unexpected payload %q", got)
	}
	if _, err := DBMongoLogin("u", "p\nq", ""); err == nil {
		t.Error("a line break in the password must be rejected")
	}
}
