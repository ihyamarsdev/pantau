package osv

import "testing"

func TestParseRuntimeProbes(t *testing.T) {
	section := "php\t8.1.2\nnode\tv20.11.0\nnginx\tnginx/1.24.0\nphp\t8.1.2"
	probes := ParseRuntimeProbes(section, "Ubuntu:22.04")
	want := map[string]RuntimePackage{
		"php":   {Name: "php", Version: "8.1.2", Ecosystem: EcoPackagist, Source: "runtime-probe"},
		"node":  {Name: "node", Version: "20.11.0", Ecosystem: EcoNPM, Source: "runtime-probe"},
		"nginx": {Name: "nginx", Version: "1.24.0", Ecosystem: "Ubuntu:22.04", Source: "runtime-probe"},
	}
	if len(probes) != len(want) {
		t.Fatalf("got %d probes, want %d: %+v", len(probes), len(want), probes)
	}
	for _, p := range probes {
		w, ok := want[p.Name]
		if !ok {
			t.Errorf("unexpected probe %+v", p)
			continue
		}
		if p.Version != w.Version || p.Ecosystem != w.Ecosystem || p.Source != w.Source {
			t.Errorf("probe %s = %+v, want %+v", p.Name, p, w)
		}
	}
}

func TestRuntimeOSVPackages(t *testing.T) {
	probes := []RuntimePackage{
		{Name: "php", Version: "8.1.2", Ecosystem: EcoPackagist},
		{Name: "node", Version: "20.0.0", Ecosystem: EcoNPM},
		{Name: "nginx", Version: "1.24", Ecosystem: "Debian:12"},
		{Name: "unknown", Version: "1", Ecosystem: ""},
	}
	byEco := RuntimeOSVPackages(probes)
	if len(byEco[EcoPackagist]) != 1 || byEco[EcoPackagist][0].Name != "php" {
		t.Errorf("packagist group = %+v", byEco[EcoPackagist])
	}
	if len(byEco[EcoNPM]) != 1 || byEco[EcoNPM][0].Name != "node" {
		t.Errorf("npm group = %+v", byEco[EcoNPM])
	}
	if len(byEco["Debian:12"]) != 1 || byEco["Debian:12"][0].Name != "nginx" {
		t.Errorf("debian group = %+v", byEco["Debian:12"])
	}
	if _, ok := byEco[""]; ok {
		t.Error("empty ecosystem should not be queried")
	}
}
