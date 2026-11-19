package osv

import (
	"strings"
	"testing"
)

func TestWebrootLockfileCmdAndParse(t *testing.T) {
	cmd := WebrootLockfileCmd([]string{"/var/www/app", "relative/path"})
	if !strings.Contains(cmd, "/var/www/app") {
		t.Errorf("command missing absolute webroot: %s", cmd)
	}
	if strings.Contains(cmd, "relative/path") {
		t.Errorf("relative path should be skipped: %s", cmd)
	}
	if !strings.Contains(cmd, "composer.lock") || !strings.Contains(cmd, "package-lock.json") {
		t.Errorf("command missing lockfile names: %s", cmd)
	}

	section := `===LOCKFILE:/var/www/app/composer.lock:composer.lock===
{"packages":[{"name":"laravel/framework","version":"v9.0.0"}]}
===LOCKFILE_END===
===LOCKFILE:/var/www/app/package-lock.json:package-lock.json===
{"lockfileVersion":2,"packages":{"node_modules/express":{"version":"4.18.1"}}}
===LOCKFILE_END===
`
	files := ParseWebrootLockfiles(section)
	if len(files) != 2 {
		t.Fatalf("got %d lockfiles, want 2: %+v", len(files), files)
	}
	composer, _, err := ParseLockfile(files[0].Name, files[0].Path, files[0].Data)
	if err != nil || len(composer) != 1 || composer[0].Name != "laravel/framework" {
		t.Errorf("composer parse = %+v err=%v", composer, err)
	}
	npm, _, err := ParseLockfile(files[1].Name, files[1].Path, files[1].Data)
	if err != nil || len(npm) != 1 || npm[0].Name != "express" {
		t.Errorf("npm parse = %+v err=%v", npm, err)
	}
}
