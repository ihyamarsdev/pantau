package ansible

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"unicode/utf8"
)

// Asset kinds. They map to the directories Ansible searches next to a playbook for `template:` and `copy:`/`lookup('file')`.
const (
	AssetTemplate = "template"
	AssetFile     = "file"

	MaxTemplateBytes    = 256 << 10
	MaxAssetFileBytes   = 5 << 20
	MaxAssetsTotalBytes = 32 << 20
	maxAssetNameLen     = 200
)

// Asset is a Jinja template or an uploaded file that a playbook task can reference by its relative name.
type Asset struct {
	Kind string
	Name string
	Data []byte
}

func assetDir(kind string) (string, bool) {
	switch kind {
	case AssetTemplate:
		return "templates", true
	case AssetFile:
		return "files", true
	}
	return "", false
}

// ValidateAsset checks the kind, the relative name (same rules as bundle paths, subfolders allowed) and the size.
func ValidateAsset(a Asset) error {
	if _, ok := assetDir(a.Kind); !ok {
		return fmt.Errorf("invalid asset kind %q: must be template or file", a.Kind)
	}
	if len(a.Name) > maxAssetNameLen {
		return fmt.Errorf("asset name exceeds %d characters", maxAssetNameLen)
	}
	if err := ValidateBundleFilePath(a.Name); err != nil {
		return fmt.Errorf("asset name: %w", err)
	}
	if a.Kind == AssetTemplate {
		if len(a.Data) > MaxTemplateBytes {
			return errors.New("template exceeds 256 KiB")
		}
		if !utf8.Valid(a.Data) {
			return errors.New("template must be valid UTF-8 text")
		}
	} else if len(a.Data) > MaxAssetFileBytes {
		return errors.New("file exceeds 5 MiB")
	}
	return nil
}

// writeAssets lays the assets out as <dir>/templates/<name> and <dir>/files/<name>. O_EXCL turns a name that is both
// a file and a folder ("a" and "a/b") into an error instead of silently losing one of them.
func writeAssets(dir string, assets []Asset) error {
	for _, a := range assets {
		sub, _ := assetDir(a.Kind)
		p := filepath.Join(dir, sub, filepath.FromSlash(a.Name))
		if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
			return fmt.Errorf("asset %s/%s: %w", sub, a.Name, err)
		}
		f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return fmt.Errorf("asset %s/%s: %w", sub, a.Name, err)
		}
		_, werr := f.Write(a.Data)
		if cerr := f.Close(); werr == nil {
			werr = cerr
		}
		if werr != nil {
			return fmt.Errorf("asset %s/%s: %w", sub, a.Name, werr)
		}
	}
	return nil
}
