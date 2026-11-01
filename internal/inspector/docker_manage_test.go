package inspector

import (
	"errors"
	"strings"
	"testing"
)

func TestDockerNameAndImageValidation(t *testing.T) {
	for _, n := range []string{"web", "web-app_1.2", "a", "0abc"} {
		if !ValidDockerName(n) {
			t.Errorf("name %q must be accepted", n)
		}
	}
	for _, n := range []string{"", "-rm", "a b", "a;b", "a$(id)", "a/b", "a\nb", ".hidden", strings.Repeat("a", 200)} {
		if ValidDockerName(n) {
			t.Errorf("name %q must be rejected", n)
		}
	}
	for _, i := range []string{"nginx", "nginx:1.25-alpine", "ghcr.io/org/app:main", "registry:5000/team/img@sha256:abc123", "sha256:abc"} {
		if !ValidDockerImage(i) {
			t.Errorf("image %q must be accepted", i)
		}
	}
	for _, i := range []string{"", "-v", "--privileged", "nginx; reboot", "a b", "$(id)", "nginx'"} {
		if ValidDockerImage(i) {
			t.Errorf("image %q must be rejected", i)
		}
	}
}

func TestDockerContainerCommand(t *testing.T) {
	cases := []struct {
		action string
		opts   DockerContainerOpts
		want   string
	}{
		{"start", DockerContainerOpts{}, "docker start 'web'"},
		{"pause", DockerContainerOpts{}, "docker pause 'web'"},
		{"unpause", DockerContainerOpts{}, "docker unpause 'web'"},
		{"kill", DockerContainerOpts{}, "docker kill 'web'"},
		{"remove", DockerContainerOpts{}, "docker rm 'web'"},
		{"remove", DockerContainerOpts{Force: true, Volumes: true}, "docker rm -f -v 'web'"},
		{"rename", DockerContainerOpts{NewName: "web2"}, "docker rename 'web' 'web2'"},
	}
	for _, c := range cases {
		got, err := dockerContainerCommand("web", c.action, c.opts)
		if err != nil || got != c.want {
			t.Errorf("%s: got %q err %v, want %q", c.action, got, err, c.want)
		}
	}
	if _, err := dockerContainerCommand("web", "exec", DockerContainerOpts{}); !errors.Is(err, ErrDockerInvalid) {
		t.Errorf("unknown action must be rejected, got %v", err)
	}
	if _, err := dockerContainerCommand("web; reboot", "start", DockerContainerOpts{}); !errors.Is(err, ErrDockerInvalid) {
		t.Errorf("bad container name must be rejected, got %v", err)
	}
	if _, err := dockerContainerCommand("web", "rename", DockerContainerOpts{NewName: "bad name"}); !errors.Is(err, ErrDockerInvalid) {
		t.Errorf("bad new name must be rejected, got %v", err)
	}
}

func TestSplitCommandLine(t *testing.T) {
	cases := map[string][]string{
		`sleep 3600`:                   {"sleep", "3600"},
		`sh -c "echo hi there"`:        {"sh", "-c", "echo hi there"},
		`sh -c 'echo "a b"; ls'`:       {"sh", "-c", `echo "a b"; ls`},
		`a\ b c`:                       {"a b", "c"},
		`  spaced   out  `:             {"spaced", "out"},
		`empty "" arg`:                 {"empty", "", "arg"},
		`$(touch /tmp/x) ` + "`id`":    {"$(touch", "/tmp/x)", "`id`"}, // no shell expansion: tokens stay inert text
		`--flag=value --other 'x y' z`: {"--flag=value", "--other", "x y", "z"},
	}
	for in, want := range cases {
		got, err := splitCommandLine(in)
		if err != nil || strings.Join(got, "\x00") != strings.Join(want, "\x00") {
			t.Errorf("%q: got %q err %v, want %q", in, got, err, want)
		}
	}
	if _, err := splitCommandLine(`echo "unterminated`); !errors.Is(err, ErrDockerInvalid) {
		t.Errorf("unterminated quote must be rejected, got %v", err)
	}
}

func TestBuildDockerRun(t *testing.T) {
	cmd, err := buildDockerRun(DockerRunSpec{
		Image: "nginx:1.25", Name: "web", Restart: "unless-stopped", Network: "appnet", Memory: "512m", CPUs: "1.5",
		Ports:   []string{"8080:80", "127.0.0.1:8443:443/tcp"},
		Env:     []string{"A=1", "MSG=hello world; rm -rf /", "EMPTY="},
		Volumes: []string{"/srv/data:/data:ro", "cache:/cache"},
		Command: `nginx -g "daemon off;"`,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `docker run -d --name 'web' --restart 'unless-stopped' --network 'appnet' -m '512m' --cpus '1.5' ` +
		`-p '8080:80' -p '127.0.0.1:8443:443/tcp' -e 'A=1' -e 'MSG=hello world; rm -rf /' -e 'EMPTY=' ` +
		`-v '/srv/data:/data:ro' -v 'cache:/cache' 'nginx:1.25' 'nginx' '-g' 'daemon off;'`
	if cmd != want {
		t.Errorf("unexpected command\n got: %s\nwant: %s", cmd, want)
	}

	// Values are inert text for the shell: a hostile value must stay inside its single quotes.
	cmd, err = buildDockerRun(DockerRunSpec{Image: "alpine", Env: []string{"X=it's $(id) `id`"}})
	if err != nil || !strings.Contains(cmd, `-e 'X=it'\''s $(id) `+"`id`"+`'`) {
		t.Errorf("hostile env value must be escaped, got %q err %v", cmd, err)
	}

	bad := []DockerRunSpec{
		{Image: ""},
		{Image: "--privileged"},
		{Image: "alpine; reboot"},
		{Image: "alpine", Name: "bad name"},
		{Image: "alpine", Restart: "sometimes"},
		{Image: "alpine", Network: "x;y"},
		{Image: "alpine", Memory: "lots"},
		{Image: "alpine", CPUs: "1;2"},
		{Image: "alpine", Ports: []string{"80"}},
		{Image: "alpine", Ports: []string{"8080:80; id"}},
		{Image: "alpine", Env: []string{"NOEQUALS"}},
		{Image: "alpine", Env: []string{"1BAD=x"}},
		{Image: "alpine", Env: []string{"A=line1\nline2"}},
		{Image: "alpine", Volumes: []string{"./relative:/data"}},
		{Image: "alpine", Volumes: []string{"/data"}},
		{Image: "alpine", Volumes: []string{"/a:/b:weird"}},
		{Image: "alpine", Volumes: []string{"/a':/b"}},
		{Image: "alpine", Command: `echo "open`},
	}
	for i, s := range bad {
		if _, err := buildDockerRun(s); !errors.Is(err, ErrDockerInvalid) {
			t.Errorf("case %d (%+v) must be rejected, got %v", i, s, err)
		}
	}
}

func TestBuildDockerNetworkCreate(t *testing.T) {
	got, err := buildDockerNetworkCreate(DockerNetworkSpec{Name: "appnet", Driver: "bridge", Subnet: "172.30.0.0/16", Gateway: "172.30.0.1", Internal: true})
	want := `docker network create --driver 'bridge' --subnet '172.30.0.0/16' --gateway '172.30.0.1' --internal 'appnet'`
	if err != nil || got != want {
		t.Errorf("got %q err %v, want %q", got, err, want)
	}
	for _, s := range []DockerNetworkSpec{
		{Name: "bad name"}, {Name: "n", Driver: "host"}, {Name: "n", Subnet: "not-a-cidr"},
		{Name: "n", Gateway: "10.0.0.1"}, {Name: "n", Subnet: "10.0.0.0/8", Gateway: "1;2"},
	} {
		if _, err := buildDockerNetworkCreate(s); !errors.Is(err, ErrDockerInvalid) {
			t.Errorf("%+v must be rejected, got %v", s, err)
		}
	}
}

const inspectFixture = `WARNING: something
[{
 "Id": "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789",
 "Name": "/web", "Created": "2026-11-01T10:00:00Z", "Image": "sha256:1111", "RestartCount": 2,
 "State": {"Status": "running", "Running": true, "Paused": false, "Restarting": false, "OOMKilled": false, "ExitCode": 0,
   "StartedAt": "2026-11-01T10:00:01Z", "FinishedAt": "0001-01-01T00:00:00Z",
   "Health": {"Status": "healthy", "Log": [{"Output": "old"}, {"Output": "ok\n"}]}},
 "Config": {"Hostname": "abc", "User": "nginx", "WorkingDir": "/app", "Image": "nginx:1.25",
   "Cmd": ["nginx", "-g", "daemon off;"], "Entrypoint": ["/docker-entrypoint.sh"],
   "Env": ["PATH=/usr/bin", "SECRET=s3"], "Labels": {"com.docker.compose.project": "shop"}},
 "HostConfig": {"RestartPolicy": {"Name": "on-failure", "MaximumRetryCount": 5}, "Memory": 536870912, "NanoCpus": 1500000000,
   "Privileged": false, "NetworkMode": "appnet", "PortBindings": {}},
 "Mounts": [{"Type": "volume", "Name": "data", "Source": "/var/lib/docker/volumes/data/_data", "Destination": "/data", "RW": true},
            {"Type": "bind", "Source": "/srv", "Destination": "/srv", "RW": false}],
 "NetworkSettings": {"Ports": {"80/tcp": [{"HostIp": "0.0.0.0", "HostPort": "8080"}], "443/tcp": null},
   "Networks": {"appnet": {"IPAddress": "172.30.0.2", "Gateway": "172.30.0.1", "MacAddress": "02:42", "Aliases": ["web"]}}}
}]`

func TestParseDockerInspect(t *testing.T) {
	d, err := parseDockerInspect(inspectFixture)
	if err != nil {
		t.Fatal(err)
	}
	if d.Name != "web" || d.Image != "nginx:1.25" || !d.Running || d.RestartCount != 2 || d.Health != "healthy" || d.HealthLog != "ok" {
		t.Errorf("basic fields wrong: %+v", d)
	}
	if d.Command != "nginx -g daemon off;" || d.Entrypoint != "/docker-entrypoint.sh" || d.User != "nginx" || d.WorkingDir != "/app" {
		t.Errorf("process fields wrong: %+v", d)
	}
	if d.RestartPolicy != "on-failure:5" || d.MemoryLimit != 536870912 || d.CPUs != 1.5 || d.NetworkMode != "appnet" {
		t.Errorf("host config wrong: %+v", d)
	}
	if len(d.Ports) != 2 || d.Ports[0].ContainerPort != "443/tcp" || d.Ports[0].HostPort != "" || d.Ports[1].HostPort != "8080" || d.Ports[1].HostIP != "0.0.0.0" {
		t.Errorf("ports wrong (sorted, exposed-only kept): %+v", d.Ports)
	}
	if len(d.Mounts) != 2 || d.Mounts[0].Name != "data" || !d.Mounts[0].ReadWrite || d.Mounts[1].ReadWrite {
		t.Errorf("mounts wrong: %+v", d.Mounts)
	}
	if len(d.Networks) != 1 || d.Networks[0].IP != "172.30.0.2" || d.Networks[0].Aliases[0] != "web" {
		t.Errorf("networks wrong: %+v", d.Networks)
	}
	if d.Labels["com.docker.compose.project"] != "shop" || len(d.Env) != 2 {
		t.Errorf("env/labels wrong: %+v", d)
	}
	if _, err := parseDockerInspect("not json"); err == nil {
		t.Error("garbage must be an error")
	}
}

func TestParseDockerImagesMarksUsedAndDangling(t *testing.T) {
	out := `===IMAGES===
{"ID":"sha256:aaaaaaaaaaaaaaaaaaaa","Repository":"nginx","Tag":"1.25","Size":"187MB","CreatedAt":"2026-10-01 10:00:00 +0000 UTC","CreatedSince":"4 weeks ago"}
{"ID":"sha256:bbbbbbbbbbbbbbbbbbbb","Repository":"<none>","Tag":"<none>","Size":"1.2GB","CreatedSince":"2 days ago"}
{"ID":"sha256:cccccccccccccccccccc","Repository":"alpine","Tag":"latest","Size":"14MB","CreatedSince":"1 month ago"}
===USED===
sha256:aaaaaaaaaaaaaaaaaaaa
`
	imgs := parseDockerImages(out)
	if len(imgs) != 3 {
		t.Fatalf("expected 3 images, got %+v", imgs)
	}
	if imgs[0].Repository != "alpine" || imgs[1].Repository != "nginx" || imgs[2].Repository != "<none>" {
		t.Errorf("order must be repo asc with dangling last: %+v", imgs)
	}
	byRepo := map[string]DockerImage{}
	for _, i := range imgs {
		byRepo[i.Repository] = i
	}
	if !byRepo["nginx"].InUse || byRepo["alpine"].InUse || byRepo["nginx"].ID != "aaaaaaaaaaaa" || byRepo["nginx"].Size != 187_000_000 {
		t.Errorf("in-use / short id / size wrong: %+v", byRepo)
	}
	if !byRepo["<none>"].Dangling || byRepo["<none>"].InUse {
		t.Errorf("dangling image wrong: %+v", byRepo["<none>"])
	}
}

func TestParseDockerStacks(t *testing.T) {
	out := `running|shop|web|/srv/shop|/srv/shop/docker-compose.yml
running|shop|db|/srv/shop|/srv/shop/docker-compose.yml
exited|shop|worker|/srv/shop|/srv/shop/docker-compose.yml
exited|old|app|/srv/old|/srv/old/a.yml,/srv/old/b.yml
running|||
running|solo|only|/x|/x/c.yml
`
	stacks := parseDockerStacks(out)
	if len(stacks) != 3 {
		t.Fatalf("expected 3 stacks (containers without a project are ignored), got %+v", stacks)
	}
	by := map[string]DockerStack{}
	for _, s := range stacks {
		by[s.Name] = s
	}
	if s := by["shop"]; s.Total != 3 || s.Running != 2 || s.Status != "partial" || strings.Join(s.Services, ",") != "db,web,worker" {
		t.Errorf("shop wrong: %+v", s)
	}
	if s := by["old"]; s.Status != "stopped" || len(s.ConfigFiles) != 2 {
		t.Errorf("old wrong: %+v", s)
	}
	if s := by["solo"]; s.Status != "running" {
		t.Errorf("solo wrong: %+v", s)
	}
}

func TestComposeStackScriptQuotesPathsAndChainsUpdate(t *testing.T) {
	st := &DockerStack{Name: "shop", WorkingDir: "/srv/my shop", ConfigFiles: []string{"/srv/my shop/a.yml", "/srv/my shop/b.yaml"}}
	script, err := composeStackScript(st, "update")
	if err != nil {
		t.Fatal(err)
	}
	base := `$C -p 'shop' -f '/srv/my shop/a.yml' -f '/srv/my shop/b.yaml'`
	if !strings.Contains(script, base+" pull && "+base+" up -d") {
		t.Errorf("update must pull then up with every -f quoted inline:\n%s", script)
	}
	if !strings.Contains(script, `cd '/srv/my shop'`) {
		t.Errorf("must run from the stack's working directory:\n%s", script)
	}

	if _, err := composeStackScript(st, "destroy"); !errors.Is(err, ErrDockerInvalid) {
		t.Errorf("unknown action must be rejected, got %v", err)
	}
	if _, err := composeStackScript(&DockerStack{Name: "x"}, "up"); err == nil {
		t.Error("a stack without a recorded compose file cannot be managed")
	}
	for _, f := range []string{"/etc/passwd", "relative.yml", "/a/../etc/x.yml", "/a/b.yml'; id; '", "/a/b\n.yml"} {
		if _, err := composeStackScript(&DockerStack{Name: "x", ConfigFiles: []string{f}}, "up"); err == nil {
			t.Errorf("compose file %q must be rejected", f)
		}
	}
}

func TestComposeDeployScript(t *testing.T) {
	script := composeDeployScript("shop", `"$HOME/.pantau/stacks/shop"`, `"$DIR/docker-compose.yml"`, "YWJj", false)
	for _, want := range []string{`P='shop'`, `printf '%s' 'YWJj'`, `config -q`, `up -d`} {
		if !strings.Contains(script, want) {
			t.Errorf("deploy script missing %q:\n%s", want, script)
		}
	}
	if strings.Contains(script, ".pantau-bak") {
		t.Error("a new stack must not create a backup")
	}
	if up := composeDeployScript("shop", `'/srv/shop'`, `'/srv/shop/c.yml'`, "YWJj", true); !strings.Contains(up, `cp -p "$FILE" "$FILE.pantau-bak"`) {
		t.Errorf("redeploying an existing file must back it up first:\n%s", up)
	}
}

func TestParseDockerSystem(t *testing.T) {
	out := `===INFO===
{"ServerVersion":"29.0.1","OperatingSystem":"Arch Linux","Architecture":"x86_64","KernelVersion":"7.2","NCPU":8,"MemTotal":16000000000,"Containers":5,"ContainersRunning":3,"ContainersPaused":0,"ContainersStopped":2,"Images":9,"Driver":"overlayfs","LoggingDriver":"json-file","CgroupDriver":"systemd","DockerRootDir":"/var/lib/docker","SecurityOptions":["name=seccomp,profile=builtin","name=rootless"],"Swarm":{"LocalNodeState":"inactive"}}
===DF===
{"Type":"Images","TotalCount":"9","Active":"4","Size":"20GB","Reclaimable":"8GB (40%)"}
{"Type":"Containers","TotalCount":"5","Active":"3","Size":"1GB","Reclaimable":"0B"}
===VERSION===
29.0.1|29.0.1
===COMPOSE===
5.5.1
`
	s, err := parseDockerSystem(out)
	if err != nil {
		t.Fatal(err)
	}
	if s.ServerVersion != "29.0.1" || s.ClientVersion != "29.0.1" || s.ComposeVersion != "5.5.1" || s.CPUs != 8 || s.ContainersRunning != 3 || !s.Rootless {
		t.Errorf("wrong: %+v", s)
	}
	if len(s.Usage) != 2 || s.Usage[0].Type != "Images" || s.Usage[0].Reclaimable != "8GB (40%)" {
		t.Errorf("usage wrong: %+v", s.Usage)
	}
	if _, err := parseDockerSystem("===ERROR===\nCannot connect to the Docker daemon\n"); err == nil || !strings.Contains(err.Error(), "Cannot connect") {
		t.Errorf("daemon error must surface, got %v", err)
	}
}

func TestDockerPruneHasNoEverythingTarget(t *testing.T) {
	for _, bad := range []string{"all", "system", "everything", ""} {
		if _, err := (&Inspector{}).DockerPrune(1, bad); !errors.Is(err, ErrDockerInvalid) {
			t.Errorf("prune target %q must be rejected, got %v", bad, err)
		}
	}
	for target, cmd := range dockerPruneCommands {
		if strings.Contains(cmd, "system prune") {
			t.Errorf("target %s must not run `docker system prune`: %s", target, cmd)
		}
	}
}

func TestDockerOpsRejectBadInputBeforeTouchingTheHost(t *testing.T) {
	ins := &Inspector{} // no db: validation must fail first
	checks := []func() error{
		func() error { _, e := ins.DockerImageOp(1, "pull", "-x", false); return e },
		func() error { _, e := ins.DockerImageOp(1, "explode", "alpine", false); return e },
		func() error { _, e := ins.DockerVolumeOp(1, "create", "bad name"); return e },
		func() error { _, e := ins.DockerVolumeOp(1, "nuke", ""); return e },
		func() error {
			_, e := ins.DockerNetworkOp(1, "connect", DockerNetworkSpec{Name: "n"}, "bad;c")
			return e
		},
		func() error { _, e := ins.DockerRunContainer(1, DockerRunSpec{Image: "x; id"}); return e },
		func() error { _, e := ins.DockerStackDeploy(1, "Bad Name", "services: {}"); return e },
		func() error { _, e := ins.DockerStackDeploy(1, "ok", "   "); return e },
		func() error {
			_, e := ins.DockerStackDeploy(1, "ok", strings.Repeat("a", dockerStackFileMax+1))
			return e
		},
	}
	for i, c := range checks {
		if err := c(); !errors.Is(err, ErrDockerInvalid) {
			t.Errorf("check %d must fail validation, got %v", i, err)
		}
	}
}

// With a Docker socket that only privileged users may use, the prelude must make `docker` run through
// passwordless sudo, and must leave it alone when sudo is not available.
func TestDockerSudoPreludeFallsBackToSudoOnlyOnPermissionDenied(t *testing.T) {
	body := DarwinPathPrelude + dockerSudoPrelude + `docker ps -a 2>&1; echo "rc=$?"` + "\n"

	if out := runShimmed(t, body, "0", false); !strings.Contains(out, "docker ps -a") || !strings.Contains(out, "rc=0") {
		t.Errorf("root must use docker directly: %s", out)
	}
	if out := runShimmed(t, body, "1000", true); !strings.Contains(out, "docker ps -a") || !strings.Contains(out, "rc=0") {
		t.Errorf("non-root with passwordless sudo must be elevated: %s", out)
	}
	out := runShimmed(t, body, "1000", false)
	if !strings.Contains(out, "permission denied") || !strings.Contains(out, "rc=1") {
		t.Errorf("without sudo the real permission error must reach the user: %s", out)
	}
}
