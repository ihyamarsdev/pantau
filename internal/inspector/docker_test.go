package inspector

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseDockerSizeBytes(t *testing.T) {
	tests := []struct {
		in   string
		want int64
	}{
		{"29.42MiB", 30849105},
		{"15.38GiB", 16514149253},
		{"512KiB", 524288},
		{"0B", 0},
		{"87MB", 87000000},
		{"590kB", 590000},
		{" 1.5GB ", 1500000000},
		{"--", 0},
		{"", 0},
	}
	for _, tc := range tests {
		if got := parseDockerSizeBytes(tc.in); got != tc.want {
			t.Errorf("parseDockerSizeBytes(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestParseDockerStatsSortsByCPUAndToleratesNoise(t *testing.T) {
	out := `WARNING: your kernel does not support swap limit capabilities
{"BlockIO":"417MB / 590kB","CPUPerc":"0.17%","Container":"4796f87064f1","ID":"4796f87064f1","MemPerc":"4.45%","MemUsage":"701.5MiB / 15.38GiB","Name":"open-webui","NetIO":"51.9kB / 12.7kB","PIDs":"13"}
{"BlockIO":"0B / 87MB","CPUPerc":"12.50%","Container":"f12e1f8a6c19","ID":"f12e1f8a6c19","MemPerc":"0.19%","MemUsage":"29.42MiB / 15.38GiB","Name":"pantau","NetIO":"5.39MB / 8.66MB","PIDs":"24"}
{"BlockIO":"0B / 0B","CPUPerc":"--","Container":"x","ID":"x","MemPerc":"--","MemUsage":"0B / 0B","Name":"paused-one","NetIO":"0B / 0B","PIDs":"0"}
not json at all
`
	stats := parseDockerStats(out)
	if len(stats) != 3 {
		t.Fatalf("expected 3 containers, got %d: %+v", len(stats), stats)
	}
	if stats[0].Name != "pantau" || stats[0].CPUPercent != 12.5 || stats[0].PIDs != 24 {
		t.Errorf("expected highest CPU first, got %+v", stats[0])
	}
	if stats[0].MemUsedBytes != 30849105 || stats[0].MemLimitBytes != 16514149253 || stats[0].MemPercent != 0.19 {
		t.Errorf("unexpected memory parse: %+v", stats[0])
	}
	if stats[2].Name != "paused-one" || stats[2].CPUPercent != 0 || stats[2].MemPercent != 0 {
		t.Errorf("'--' values must parse to 0, got %+v", stats[2])
	}
	if empty := parseDockerStats(""); empty == nil || len(empty) != 0 {
		t.Errorf("empty output must yield an empty, non-nil list")
	}
}

const dockerVolumesFixture = `===VOLUMES===
[
  {"CreatedAt":"2026-01-23T10:55:57+07:00","Driver":"local","Labels":{"com.docker.compose.project":"chat"},"Mountpoint":"/var/lib/docker/volumes/chat_db-data/_data","Name":"chat_db-data","Options":null,"Scope":"local"},
  {"CreatedAt":"2026-03-04T09:45:06+07:00","Driver":"local","Labels":null,"Mountpoint":"/var/lib/docker/volumes/orphan/_data","Name":"orphan","Options":null,"Scope":"local"}
]
===USERS===
/chat-db-1|volume:chat_db-data,
/chat-web-1|volume:chat_db-data,bind:,
/pantau|bind:,
===DF===
[{"Name":"chat_db-data","Size":"48.59MB","Links":"2","Driver":"local"},{"Name":"orphan","Size":"N/A","Links":"0","Driver":"local"}]
`

func TestParseDockerVolumesJoinsUsageAndSizes(t *testing.T) {
	vols, err := parseDockerVolumes(dockerVolumesFixture)
	if err != nil {
		t.Fatal(err)
	}
	if len(vols) != 2 || vols[0].Name != "chat_db-data" || vols[1].Name != "orphan" {
		t.Fatalf("unexpected volumes: %+v", vols)
	}
	if !reflect.DeepEqual(vols[0].UsedBy, []string{"chat-db-1", "chat-web-1"}) {
		t.Errorf("used_by must list containers mounting the volume (bind mounts excluded), got %v", vols[0].UsedBy)
	}
	if vols[0].Size != "48.59MB" || vols[0].Labels["com.docker.compose.project"] != "chat" {
		t.Errorf("unexpected size/labels: %+v", vols[0])
	}
	if vols[1].Size != "" || vols[1].UsedBy == nil || len(vols[1].UsedBy) != 0 || vols[1].Labels == nil {
		t.Errorf("orphan volume: unknown size must be empty and slices/maps non-nil, got %+v", vols[1])
	}
}

func TestParseDockerVolumesPutsAnonymousVolumesLast(t *testing.T) {
	hash := strings.Repeat("a1", 32)
	out := "===VOLUMES===\n[" +
		`{"Driver":"local","Labels":{"com.docker.volume.anonymous":""},"Mountpoint":"/v/x","Name":"zz-labelled","Scope":"local"},` +
		`{"Driver":"local","Labels":null,"Mountpoint":"/v/h","Name":"` + hash + `","Scope":"local"},` +
		`{"Driver":"local","Labels":null,"Mountpoint":"/v/n","Name":"zz-named","Scope":"local"},` +
		`{"Driver":"local","Labels":null,"Mountpoint":"/v/a","Name":"aa-named","Scope":"local"}` +
		"]\n===USERS===\n===DF===\n"
	vols, err := parseDockerVolumes(out)
	if err != nil || len(vols) != 4 {
		t.Fatalf("expected 4 volumes, got %+v err=%v", vols, err)
	}
	got := []string{vols[0].Name, vols[1].Name, vols[2].Name, vols[3].Name}
	want := []string{"aa-named", "zz-named", hash, "zz-labelled"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("named volumes first, anonymous (hash name or anonymous label) last: got %v want %v", got, want)
	}
	if vols[0].Anonymous || vols[1].Anonymous || !vols[2].Anonymous || !vols[3].Anonymous {
		t.Errorf("unexpected anonymous flags: %+v", vols)
	}
}

func TestParseDockerVolumesWithoutSizeSupportStillLists(t *testing.T) {
	// Older engines have no `system df -v --format`: the DF section is empty and sizes stay unknown.
	out := "===VOLUMES===\n[{\"Driver\":\"local\",\"Mountpoint\":\"/v/a\",\"Name\":\"a\",\"Scope\":\"local\"}]\n===USERS===\n===DF===\n"
	vols, err := parseDockerVolumes(out)
	if err != nil || len(vols) != 1 || vols[0].Size != "" {
		t.Fatalf("expected one volume with unknown size, got %+v err=%v", vols, err)
	}
	empty, err := parseDockerVolumes("===VOLUMES===\n[]\n===USERS===\n===DF===\n")
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("no volumes must be an empty list, got %+v err=%v", empty, err)
	}
}

func TestParseDockerVolumesAndNetworksSurfaceDockerErrors(t *testing.T) {
	denied := "===ERROR===\npermission denied while trying to connect to the Docker daemon socket\n"
	if _, err := parseDockerVolumes(denied); err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Errorf("volumes must surface the docker error, got %v", err)
	}
	if _, err := parseDockerNetworks(denied); err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Errorf("networks must surface the docker error, got %v", err)
	}
}

func TestParseDockerNetworks(t *testing.T) {
	out := `===NETWORKS===
[
 {"Name":"bridge","Id":"bd42b76703ca34791be768950e7157fc63a50373252b02319254085d4ceb338e","Driver":"bridge","Scope":"local","Internal":false,"EnableIPv6":false,
  "IPAM":{"Config":[{"Subnet":"172.17.0.0/16","Gateway":"172.17.0.1"}]},
  "Containers":{"549b":{"Name":"mailpit","IPv4Address":"172.17.0.2/16","IPv6Address":""}}},
 {"Name":"chat_default","Id":"1111111111112222","Driver":"bridge","Scope":"local","Internal":true,"EnableIPv6":true,
  "IPAM":{"Config":[{"Subnet":"172.20.0.0/16","Gateway":"172.20.0.1"},{"Subnet":"fd00::/64"}]},
  "Containers":{"b":{"Name":"web","IPv4Address":"172.20.0.3/16","IPv6Address":""},"a":{"Name":"db","IPv4Address":"172.20.0.2/16","IPv6Address":"fd00::2/64"}}},
 {"Name":"host","Id":"cccccccccccccccc","Driver":"host","Scope":"local","IPAM":{"Config":[]},"Containers":{}}
]
`
	nets, err := parseDockerNetworks(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(nets) != 3 {
		t.Fatalf("expected 3 networks, got %+v", nets)
	}
	// User-defined networks come first, built-in (bridge/host/none) last.
	if nets[0].Name != "chat_default" || nets[0].BuiltIn || nets[1].Name != "bridge" || !nets[1].BuiltIn || nets[2].Name != "host" {
		t.Errorf("unexpected ordering: %v %v %v", nets[0].Name, nets[1].Name, nets[2].Name)
	}
	c := nets[0]
	if c.ID != "111111111111" || !c.Internal || !c.IPv6 || !reflect.DeepEqual(c.Subnets, []string{"172.20.0.0/16", "fd00::/64"}) || !reflect.DeepEqual(c.Gateways, []string{"172.20.0.1"}) {
		t.Errorf("unexpected network details: %+v", c)
	}
	if len(c.Containers) != 2 || c.Containers[0].Name != "db" || c.Containers[0].IPv6 != "fd00::2/64" || c.Containers[1].Name != "web" {
		t.Errorf("containers must be sorted by name with addresses: %+v", c.Containers)
	}
	if nets[2].Subnets == nil || nets[2].Containers == nil || len(nets[2].Subnets) != 0 {
		t.Errorf("empty slices must be non-nil for JSON: %+v", nets[2])
	}
}

func TestDockerScriptsRunUnderPOSIXShellWithPathPrelude(t *testing.T) {
	// zsh (macOS login shell) does not word-split unquoted variables, so the multi-line scripts are run via `sh -c`
	// and must carry the Homebrew/Docker Desktop PATH prelude and a size-scan timeout guard.
	for name, s := range map[string]string{"volumes": dockerVolumesScript, "networks": dockerNetworksScript} {
		if !strings.HasPrefix(s, DarwinPathPrelude) {
			t.Errorf("%s script must start with DarwinPathPrelude", name)
		}
	}
	if !strings.Contains(dockerVolumesScript, `T="timeout 40"`) {
		t.Errorf("volume size scan must be bounded when `timeout` exists")
	}
}
