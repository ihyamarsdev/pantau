package inspector

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
)

// Database backup: find the database servers of a host (installed on the host
// itself or running in Docker containers) and stream a full dump of one of
// them. MySQL/MariaDB, PostgreSQL and MongoDB are supported. Nothing is stored
// on the host or on the Pantau server: the dump is written to the caller.

const dbScanTimeout = 30 * time.Second

// DBSource is one database server a host can be backed up from.
type DBSource struct {
	Engine    string `json:"engine"`    // mysql (also MariaDB) | postgres | mongo
	Where     string `json:"where"`     // host | container
	Container string `json:"container"` // container name, empty on the host
	Image     string `json:"image"`
	Missing   string `json:"missing"` // dump tool the host lacks (host sources only)
}

// DBEngineValid reports whether engine is one Pantau can dump.
func DBEngineValid(engine string) bool {
	return engine == "mysql" || engine == "postgres" || engine == "mongo"
}

// dbScanScript prints BIN (dump tools on the PATH), PROC (database server
// processes that run outside any container; a containerized server shows up in
// the host process list too) and CTR (running containers) lines.
const dbScanScript = `for b in mysqldump mariadb-dump pg_dumpall mongodump; do command -v "$b" >/dev/null 2>&1 && echo "BIN $b"; done
ps -eo pid=,comm= 2>/dev/null | while read pid comm; do
  case "$comm" in mysqld|mariadbd|postgres|postmaster|mongod) ;; *) continue;; esac
  grep -qaE 'docker|containerd|kubepods|libpod' "/proc/$pid/cgroup" 2>/dev/null && continue
  echo "PROC $comm"
done | sort -u
command -v docker >/dev/null 2>&1 && docker ps --format 'CTR {{.Names}}|{{.Image}}' 2>/dev/null
true
`

// ScanDatabases lists the database servers of a host.
func (ins *Inspector) ScanDatabases(hostID int64) ([]DBSource, error) {
	runner, err := ins.dockerRunner(hostID)
	if err != nil {
		return nil, err
	}
	defer runner.Close()
	stdout, _, _, err := runner.ExecTimeout(dockerShell(dbScanScript), dbScanTimeout)
	if err != nil {
		return nil, err
	}
	return parseDBScan(stdout), nil
}

func parseDBScan(out string) []DBSource {
	bins, procs := map[string]bool{}, map[string]bool{}
	var containers []DBSource
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "BIN "):
			bins[strings.TrimPrefix(line, "BIN ")] = true
		case strings.HasPrefix(line, "PROC "):
			procs[strings.TrimPrefix(line, "PROC ")] = true
		case strings.HasPrefix(line, "CTR "):
			name, image, ok := strings.Cut(strings.TrimPrefix(line, "CTR "), "|")
			if !ok || !ValidDockerName(name) {
				continue
			}
			if engine := dbEngineForImage(image); engine != "" {
				containers = append(containers, DBSource{Engine: engine, Where: "container", Container: name, Image: image})
			}
		}
	}

	var out2 []DBSource
	host := func(engine string, running bool, tools ...string) {
		if !running {
			return
		}
		src := DBSource{Engine: engine, Where: "host"}
		have := false
		for _, t := range tools {
			have = have || bins[t]
		}
		if !have {
			src.Missing = tools[0]
		}
		out2 = append(out2, src)
	}
	host("mysql", procs["mysqld"] || procs["mariadbd"], "mysqldump", "mariadb-dump")
	host("postgres", procs["postgres"] || procs["postmaster"], "pg_dumpall")
	host("mongo", procs["mongod"], "mongodump")

	sort.Slice(containers, func(i, j int) bool { return containers[i].Container < containers[j].Container })
	return append(out2, containers...)
}

// dbEngineForImage maps a container image to a database engine, or "" for
// anything else (exporters, admin UIs and backup helpers carry the engine's
// name too and must not be offered).
func dbEngineForImage(image string) string {
	base := strings.ToLower(image)
	if i := strings.LastIndex(base, "/"); i >= 0 {
		base = base[i+1:]
	}
	if i := strings.IndexAny(base, ":@"); i >= 0 {
		base = base[:i]
	}
	for _, skip := range []string{"exporter", "express", "admin", "ui", "backup", "proxy", "router", "operator", "exec"} {
		if strings.Contains(base, skip) {
			return ""
		}
	}
	switch {
	case strings.HasPrefix(base, "postgres"), base == "postgis", strings.Contains(base, "timescaledb"):
		return "postgres"
	case strings.HasPrefix(base, "mysql"), strings.HasPrefix(base, "mariadb"), strings.HasPrefix(base, "percona"):
		return "mysql"
	case strings.HasPrefix(base, "mongo"):
		return "mongo"
	}
	return ""
}

// dbHostPrelude sets SUDO when the SSH user is not root but may sudo without a password.
const dbHostPrelude = `SUDO=""; if [ "$(id -u)" != 0 ] && sudo -n true 2>/dev/null; then SUDO="sudo -n"; fi
`

// dbMongoDumpTail runs mongodump with the options in "$@" and the login read into U/P/A.
// The password goes through a config file descriptor, never onto a command line (where
// other local users could read it).
const dbMongoDumpTail = `if [ -n "$U" ]; then
  P=$(printf '%s' "$P" | sed 's/\\/\\\\/g; s/"/\\"/g')
  "$D" --archive "$@" --username "$U" --authenticationDatabase "${A:-admin}" --config /dev/fd/3 3<<EOF
password: "$P"
EOF
else
  "$D" --archive "$@"
fi
`

// Host dumps rely on the local authentication of the database (root over the
// unix socket for MySQL/MariaDB, the postgres OS user for PostgreSQL).
var dbHostScripts = map[string]string{
	"mysql": `D=$(command -v mariadb-dump || command -v mysqldump) || { echo "mysqldump or mariadb-dump not found" >&2; exit 127; }
exec $SUDO "$D" --all-databases --single-transaction --quick --routines --events --triggers
`,
	"postgres": `D=$(command -v pg_dumpall) || { echo "pg_dumpall not found" >&2; exit 127; }
if [ "$(id -u)" = 0 ]; then exec su -s /bin/sh postgres -c "$D"
elif [ -n "$SUDO" ]; then exec sudo -n -u postgres "$D"
else exec "$D"; fi
`,
	// MongoDB is dumped over the network port, so no sudo is needed. TLS options come from
	// mongod.conf; credentials, when given, arrive on stdin (see DBMongoLogin).
	"mongo": `IFS= read -r U; IFS= read -r P; IFS= read -r A
D=$(command -v mongodump) || { echo "mongodump not found" >&2; exit 127; }
CONF=/etc/mongod.conf
conf() { cat "$CONF" 2>/dev/null || $SUDO cat "$CONF" 2>/dev/null; }
v() { conf | sed -n "s/^[[:space:]]*$1:[[:space:]]*//p" | head -n 1 | sed "s/[[:space:]]*#.*//" | tr -d "\"'"; }
set --
case "$(v mode)" in requireTLS|preferTLS|requireSSL|preferSSL)
  set -- --ssl --sslAllowInvalidHostnames
  CA=$(v CAFile); if [ -n "$CA" ]; then set -- "$@" --sslCAFile "$CA"; fi
  K=$(v certificateKeyFile); if [ -z "$K" ]; then K=$(v PEMKeyFile); fi
  if [ -n "$K" ]; then set -- "$@" --sslPEMKeyFile "$K"; fi;;
esac
PORT=$(v port); if [ -n "$PORT" ]; then set -- "$@" --port "$PORT"; fi
B=$(v bindIp)
case ",$B," in ,,|*,127.0.0.1,*|*,localhost,*|*,0.0.0.0,*) ;; *) set -- "$@" --host "${B%%,*}";; esac
` + dbMongoDumpTail,
}

// Container dumps run the tool inside the container with the credentials of its
// official-image environment variables, which never appear on the SSH command line.
var dbContainerScripts = map[string]string{
	"mysql": `D=$(command -v mariadb-dump || command -v mysqldump) || { echo "no mysqldump or mariadb-dump in the container" >&2; exit 127; }
P="${MYSQL_ROOT_PASSWORD:-$MARIADB_ROOT_PASSWORD}"
F="${MYSQL_ROOT_PASSWORD_FILE:-$MARIADB_ROOT_PASSWORD_FILE}"
if [ -z "$P" ] && [ -n "$F" ]; then P=$(cat "$F"); fi
if [ -n "$P" ]; then export MYSQL_PWD="$P"; fi
exec "$D" -uroot --all-databases --single-transaction --quick --routines --events --triggers
`,
	"postgres": `P="$POSTGRES_PASSWORD"
if [ -z "$P" ] && [ -n "$POSTGRES_PASSWORD_FILE" ]; then P=$(cat "$POSTGRES_PASSWORD_FILE"); fi
if [ -n "$P" ]; then export PGPASSWORD="$P"; fi
exec pg_dumpall -U "${POSTGRES_USER:-postgres}"
`,
	"mongo": `IFS= read -r U; IFS= read -r P; IFS= read -r A
if [ -z "$U" ]; then
  U="$MONGO_INITDB_ROOT_USERNAME"; P="$MONGO_INITDB_ROOT_PASSWORD"
  if [ -z "$P" ] && [ -n "$MONGO_INITDB_ROOT_PASSWORD_FILE" ]; then P=$(cat "$MONGO_INITDB_ROOT_PASSWORD_FILE"); fi
fi
D=$(command -v mongodump) || { echo "no mongodump in the container" >&2; exit 127; }
set --
` + dbMongoDumpTail,
}

// DBDumpCommand builds the remote command that writes an uncompressed dump of
// one database server to stdout.
func DBDumpCommand(engine, where, container string) (string, error) {
	if !DBEngineValid(engine) {
		return "", fmt.Errorf("unsupported database engine %q", engine)
	}
	switch where {
	case "host":
		return "sh -c " + shellQuote(DarwinPathPrelude+dbHostPrelude+dbHostScripts[engine]), nil
	case "container":
		if !ValidDockerName(container) {
			return "", fmt.Errorf("invalid container name %q", container)
		}
		argv := "docker exec "
		if engine == "mongo" {
			argv += "-i " // the login arrives on stdin
		}
		return dockerShell(argv + container + " sh -c " + shellQuote(dbContainerScripts[engine])), nil
	}
	return "", fmt.Errorf("unknown database location %q", where)
}

// DBDumpExt is the file extension of a gzip-compressed dump of engine.
func DBDumpExt(engine string) string {
	if engine == "mongo" {
		return ".archive.gz" // mongorestore --archive
	}
	return ".sql.gz"
}

// DBMongoLogin is the stdin payload that gives a MongoDB dump its login: three
// lines (user, password, authentication database). Empty user means "no login
// given" and returns "". Line breaks cannot be carried and are rejected.
func DBMongoLogin(user, password, authDB string) (string, error) {
	if user == "" {
		return "", nil
	}
	for _, f := range []string{user, password, authDB} {
		if strings.ContainsAny(f, "\r\n") {
			return "", fmt.Errorf("the login must not contain line breaks")
		}
	}
	return user + "\n" + password + "\n" + authDB + "\n", nil
}

// DumpDatabase streams the dump of one database server to out; stdin is the
// optional login payload (DBMongoLogin). Cancelling ctx drops the SSH
// connection, which stops the remote dump.
func (ins *Inspector) DumpDatabase(ctx context.Context, hostID int64, engine, where, container, stdin string, out io.Writer) error {
	cmd, err := DBDumpCommand(engine, where, container)
	if err != nil {
		return err
	}
	runner, err := ins.dockerRunner(hostID)
	if err != nil {
		return err
	}
	defer runner.Close()
	defer context.AfterFunc(ctx, func() { _ = runner.Close() })()
	var in io.Reader
	if stdin != "" {
		in = strings.NewReader(stdin)
	}
	return runner.PipeCommand(cmd, in, out)
}

// DiagnoseDatabaseDump re-runs a dump that failed before producing any output
// and returns what it printed on stderr. Only call it for such early failures:
// a dump that started fine would be repeated in full.
func (ins *Inspector) DiagnoseDatabaseDump(hostID int64, engine, where, container, stdin string) string {
	cmd, err := DBDumpCommand(engine, where, container)
	if err != nil {
		return err.Error()
	}
	runner, err := ins.dockerRunner(hostID)
	if err != nil {
		return err.Error()
	}
	defer runner.Close()
	diag := "sh -c " + shellQuote(cmd+" 2>&1 >/dev/null | grep -v deprecated | head -c 600")
	if stdin == "" {
		stdout, _, _, err := runner.ExecTimeout(diag, dbScanTimeout)
		if err != nil {
			return err.Error()
		}
		return strings.TrimSpace(stdout)
	}
	var buf bytes.Buffer
	if err := runner.PipeCommand(diag, strings.NewReader(stdin), &buf); err != nil {
		return err.Error()
	}
	return strings.TrimSpace(buf.String())
}
