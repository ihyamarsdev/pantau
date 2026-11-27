package inspector

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"pantau/internal/teleport"
)

func TestKubeForwardTargetValidate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		target  KubeForwardTarget
		wantErr bool
	}{
		{name: "service", target: KubeForwardTarget{Kind: "service", Name: "web", Namespace: "default", RemotePort: 80}},
		{name: "pod with dotted subdomain name", target: KubeForwardTarget{Kind: "pod", Name: "web.v1-7f9c", Namespace: "kube-system", RemotePort: 8080}},
		{name: "unknown kind", target: KubeForwardTarget{Kind: "deployment", Name: "web", Namespace: "default", RemotePort: 80}, wantErr: true},
		{name: "uppercase name", target: KubeForwardTarget{Kind: "service", Name: "Web", Namespace: "default", RemotePort: 80}, wantErr: true},
		{name: "slash in name", target: KubeForwardTarget{Kind: "pod", Name: "web/x", Namespace: "default", RemotePort: 80}, wantErr: true},
		{name: "dot dot in name", target: KubeForwardTarget{Kind: "pod", Name: "a..b", Namespace: "default", RemotePort: 80}, wantErr: true},
		{name: "shell metacharacters in name", target: KubeForwardTarget{Kind: "pod", Name: "web;rm", Namespace: "default", RemotePort: 80}, wantErr: true},
		{name: "namespace with dot", target: KubeForwardTarget{Kind: "service", Name: "web", Namespace: "a.b", RemotePort: 80}, wantErr: true},
		{name: "port zero", target: KubeForwardTarget{Kind: "service", Name: "web", Namespace: "default", RemotePort: 0}, wantErr: true},
		{name: "port too high", target: KubeForwardTarget{Kind: "service", Name: "web", Namespace: "default", RemotePort: 70000}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.target.Validate()
			if (err != nil) != tt.wantErr {
				t.Fatalf("Validate() = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// The argv is built without a shell, so a value can only ever be one argument.
func TestKubeForwardTargetArgs(t *testing.T) {
	t.Parallel()
	svc := KubeForwardTarget{Kind: "service", Name: "web", Namespace: "prod", RemotePort: 80}
	got := svc.Args("127.0.0.1", "15001:80")
	want := []string{"port-forward", "--address", "127.0.0.1", "svc/web", "15001:80", "-n", "prod"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Args = %v, want %v", got, want)
	}
	pod := KubeForwardTarget{Kind: "pod", Name: "web-7f9c", Namespace: "prod", RemotePort: 8080}
	if ref := pod.Ref(); ref != "pod/web-7f9c" {
		t.Fatalf("Ref = %q, want pod/web-7f9c", ref)
	}
}

func TestParseForwardPort(t *testing.T) {
	t.Parallel()
	tests := []struct {
		line   string
		want   int
		wantOK bool
	}{
		{line: "Forwarding from 127.0.0.1:4242 -> 80", want: 4242, wantOK: true},
		{line: "Forwarding from [::1]:15001 -> 8080", want: 15001, wantOK: true},
		{line: "Forwarding from 127.0.0.1:0 -> 80", wantOK: false},
		{line: "Handling connection for 80", wantOK: false},
		{line: "Forwarding from 127.0.0.1:99999 -> 80", wantOK: false},
	}
	for _, tt := range tests {
		t.Run(tt.line, func(t *testing.T) {
			t.Parallel()
			got, ok := ParseForwardPort(tt.line)
			if ok != tt.wantOK || got != tt.want {
				t.Fatalf("ParseForwardPort(%q) = %d, %v; want %d, %v", tt.line, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

// fakeKubectl writes a script that behaves like kubectl port-forward: it prints the readiness line for every
// local:remote mapping, records its pid, then waits. The pid lets a test prove the process is gone after Stop.
func fakeKubectl(t *testing.T, pidFile string) string {
	t.Helper()
	script := "#!/bin/sh\n" +
		"echo \"$$\" > " + shellQuoteForTest(pidFile) + "\n" +
		"for a in \"$@\"; do\n" +
		"  case \"$a\" in\n" +
		"    *:*) echo \"Forwarding from 127.0.0.1:${a%%:*} -> ${a#*:}\";;\n" +
		"  esac\n" +
		"done\n" +
		"exec sleep 600\n"
	path := filepath.Join(t.TempDir(), "kubectl")
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func shellQuoteForTest(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

func TestStartLocalKubectlForwardStopKillsProcess(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("the fake kubectl is a shell script")
	}
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "pid")
	kubectl := fakeKubectl(t, pidFile)
	port := freeLocalPort(t)

	target := KubeForwardTarget{Kind: "service", Name: "web", Namespace: "default", RemotePort: 80}
	fwd, ready, err := StartLocalKubectlForward(context.Background(), kubectl, "apiVersion: v1\n", target, "127.0.0.1", strconv.Itoa(port)+":80")
	if err != nil {
		t.Fatalf("StartLocalKubectlForward: %v", err)
	}
	if got, ok := ParseForwardPort(ready); !ok || got != port {
		t.Fatalf("ready line %q: port = %d ok=%v, want %d", ready, got, ok, port)
	}

	raw, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	fwd.Stop()
	fwd.Stop() // idempotent: the second call must not signal a reaped process

	if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("kubectl pid %d still alive after Stop (kill 0 = %v)", pid, err)
	}
}

func TestStartLocalKubectlForwardReportsEarlyExit(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("the fake kubectl is a shell script")
	}
	kubectl := filepath.Join(t.TempDir(), "kubectl")
	script := "#!/bin/sh\necho 'error: forbidden: services is forbidden' >&2\nexit 1\n"
	if err := os.WriteFile(kubectl, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	target := KubeForwardTarget{Kind: "service", Name: "web", Namespace: "default", RemotePort: 80}
	_, _, err := StartLocalKubectlForward(context.Background(), kubectl, "cfg", target, "127.0.0.1", "15001:80")
	if err == nil || !strings.Contains(err.Error(), "forbidden") {
		t.Fatalf("error = %v, want the kubectl stderr in it", err)
	}
}

func TestStartLocalKubectlForwardRejectsInvalidTarget(t *testing.T) {
	t.Parallel()
	bad := KubeForwardTarget{Kind: "service", Name: "web;rm", Namespace: "default", RemotePort: 80}
	if _, _, err := StartLocalKubectlForward(context.Background(), "/bin/false", "cfg", bad, "127.0.0.1", "1:80"); err == nil {
		t.Fatal("an invalid target started a process")
	}
}

// A Teleport forward names its isolated kubeconfig and context on the kubectl argv, before the subcommand, and
// stopping it must not remove that file.
func TestStartTeleportKubectlForwardNamesIsolatedFileAndContext(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("the fake kubectl is a shell script")
	}
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args")
	kubeconfig := filepath.Join(dir, "profile-7.yaml")
	if err := os.WriteFile(kubeconfig, []byte("apiVersion: v1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\n" +
		"printf '%s\\n' \"$@\" > " + shellQuoteForTest(argsFile) + "\n" +
		"for a in \"$@\"; do\n" +
		"  case \"$a\" in\n" +
		"    *:*) echo \"Forwarding from 127.0.0.1:${a%%:*} -> ${a#*:}\";;\n" +
		"  esac\n" +
		"done\n" +
		"exec sleep 600\n"
	kubectl := filepath.Join(dir, "kubectl")
	if err := os.WriteFile(kubectl, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	port := freeLocalPort(t)

	target := KubeForwardTarget{Kind: "service", Name: "web", Namespace: "default", RemotePort: 80}
	fwd, _, err := StartTeleportKubectlForward(context.Background(), kubectl, kubeconfig, "tc/prod-eu", target,
		"127.0.0.1", strconv.Itoa(port)+":80")
	if err != nil {
		t.Fatalf("StartTeleportKubectlForward: %v", err)
	}

	raw, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	want := []string{"--kubeconfig", kubeconfig, "--context", "tc/prod-eu",
		"port-forward", "--address", "127.0.0.1", "svc/web", strconv.Itoa(port) + ":80", "-n", "default"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("kubectl argv = %q, want %q", got, want)
	}

	fwd.Stop()
	if _, err := os.Stat(kubeconfig); err != nil {
		t.Fatalf("Stop removed the isolated kubeconfig: %v", err)
	}
}

func TestStartTeleportKubectlForwardRejectsFlagLikeContext(t *testing.T) {
	t.Parallel()
	target := KubeForwardTarget{Kind: "service", Name: "web", Namespace: "default", RemotePort: 80}
	_, _, err := StartTeleportKubectlForward(context.Background(), "/bin/false", "/tmp/x.yaml", "-o", target, "127.0.0.1", "1:80")
	if !errors.Is(err, teleport.ErrInvalidInput) {
		t.Fatalf("error = %v, want ErrInvalidInput", err)
	}
}

// freeLocalPort asks the kernel for a free port; the forward binds it itself, so the race is harmless for a test.
func freeLocalPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}
