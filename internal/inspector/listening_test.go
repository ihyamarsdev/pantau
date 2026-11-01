package inspector

import (
	"encoding/json"
	"strings"
	"testing"

	"pantau/internal/sshrunner"
	"pantau/internal/store"
)

func hostWithPorts(t *testing.T, ports []store.ListeningPort) *store.Host {
	t.Helper()
	b, err := json.Marshal(ports)
	if err != nil {
		t.Fatal(err)
	}
	return &store.Host{ID: 7, Name: "web", OSInfo: "Ubuntu 22.04", ListeningPorts: string(b)}
}

func decodePorts(t *testing.T, h *store.Host) map[string]store.ListeningPort {
	t.Helper()
	var ports []store.ListeningPort
	if err := json.Unmarshal([]byte(h.ListeningPorts), &ports); err != nil {
		t.Fatal(err)
	}
	m := map[string]store.ListeningPort{}
	for _, p := range ports {
		m[p.Port] = p
	}
	return m
}

func TestEnrichListeningProcessesPrefersRealOwnerThenLabelledPortHint(t *testing.T) {
	h := hostWithPorts(t, []store.ListeningPort{
		{Proto: "tcp", Port: "22", Address: "0.0.0.0:22", Process: "sshd", ProcessSource: "process"},
		{Proto: "tcp", Port: "5432", Address: "127.0.0.1:5432"},
		{Proto: "tcp", Port: "3306", Address: "0.0.0.0:3306"},
		{Proto: "tcp", Port: "40000", Address: "0.0.0.0:40000"},
	})
	runner := sshrunner.NewMockRunner()
	runner.DefaultExec = func(cmd string) (string, string, int, error) {
		// passwordless sudo works and reveals the postgres owner; mysql stays unseen
		return "===SUDO===\n" + `LISTEN 127.0.0.1:5432 users:(("postgres",pid=9,fd=5))` + "\n", "", 0, nil
	}

	(&Inspector{}).enrichListeningProcesses(h, runner)
	got := decodePorts(t, h)

	if p := got["22"]; p.Process != "sshd" || p.ProcessSource != "process" {
		t.Errorf("an already-named port must be left untouched, got %+v", p)
	}
	if p := got["5432"]; p.Process != "postgres" || p.ProcessSource != "process" {
		t.Errorf("visible owner must win over the port hint, got %+v", p)
	}
	if p := got["3306"]; p.Process != "mysql" || p.ProcessSource != "port_hint" {
		t.Errorf("unseen well-known port must be a labelled hint, got %+v", p)
	}
	if p := got["40000"]; p.Process != "" || p.ProcessSource != "" {
		t.Errorf("unknown port must stay unnamed (never invent a name), got %+v", p)
	}
}

func TestEnrichListeningProcessesProbesSudoOncePerBackoffWindow(t *testing.T) {
	ins := &Inspector{}
	var scripts []string
	runner := sshrunner.NewMockRunner()
	runner.DefaultExec = func(cmd string) (string, string, int, error) {
		scripts = append(scripts, cmd)
		return "===NOPRIV===\n", "", 0, nil
	}

	for i := range 3 {
		h := hostWithPorts(t, []store.ListeningPort{{Proto: "tcp", Port: "80", Address: "0.0.0.0:80"}})
		ins.enrichListeningProcesses(h, runner)
		if p := decodePorts(t, h)["80"]; p.Process != "http" || p.ProcessSource != "port_hint" {
			t.Fatalf("run %d: expected labelled http hint, got %+v", i, p)
		}
	}

	if len(scripts) != 3 {
		t.Fatalf("expected one enrichment exec per inspection, got %d", len(scripts))
	}
	if !strings.Contains(scripts[0], "TRY=1") {
		t.Errorf("first inspection must probe passwordless sudo")
	}
	for i, s := range scripts[1:] {
		if !strings.Contains(s, "TRY=0") {
			t.Errorf("inspection %d must not re-probe sudo inside the backoff window", i+2)
		}
	}
}

func TestEnrichListeningProcessesSkipsWhenEveryPortIsNamed(t *testing.T) {
	h := hostWithPorts(t, []store.ListeningPort{{Proto: "tcp", Port: "22", Address: "0.0.0.0:22", Process: "sshd", ProcessSource: "process"}})
	runner := sshrunner.NewMockRunner()
	(&Inspector{}).enrichListeningProcesses(h, runner)
	if len(runner.ExecHistory) != 0 {
		t.Errorf("no extra SSH command expected when all owners are known, got %v", runner.ExecHistory)
	}
}
