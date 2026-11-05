package ansible

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	// MaxBundleSize limits both ZIP bytes and total expanded bytes of an uploaded folder.
	MaxBundleSize = 64 << 20
	// MaxBundleFiles limits ZIP entries, including explicit directories.
	MaxBundleFiles = 10000
)

// ValidateBundleFilePath accepts clean, portable relative file names, including hidden dependencies.
// ZIP directory entries use the same validation after removing their trailing slash.
func ValidateBundleFilePath(name string) error {
	// Browser multipart percent-escapes quotes in filenames; reject them to preserve exact dependency paths.
	if name == "" || !utf8.ValidString(name) || strings.ContainsAny(name, "\\:\"") || strings.HasPrefix(name, "/") || path.Clean(name) != name || name == "." || name == ".." || strings.HasPrefix(name, "../") {
		return errors.New("bundle file path must be a clean relative path inside the project")
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return errors.New("bundle file path must not contain control characters")
		}
	}
	for _, segment := range strings.Split(name, "/") {
		if strings.EqualFold(segment, ".git") {
			return errors.New("bundle file path must not contain a .git segment")
		}
	}
	return nil
}

// ValidateArchive checks every entry, including its decompressed contents and ZIP checksum, before extraction,
// and returns the entries by path.
func ValidateArchive(archive []byte) (map[string]*zip.File, error) {
	if len(archive) > MaxBundleSize {
		return nil, errors.New("bundle archive exceeds 64 MiB")
	}
	zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return nil, fmt.Errorf("invalid bundle ZIP: %w", err)
	}
	if len(zr.File) > MaxBundleFiles {
		return nil, fmt.Errorf("bundle exceeds maximum of %d files", MaxBundleFiles)
	}
	entries := make(map[string]*zip.File, len(zr.File))
	var total uint64
	for _, f := range zr.File {
		name := strings.TrimSuffix(f.Name, "/")
		if err := ValidateBundleFilePath(name); err != nil {
			return nil, fmt.Errorf("bundle entry %q: %w", f.Name, err)
		}
		if _, exists := entries[name]; exists {
			return nil, fmt.Errorf("duplicate bundle path %q", name)
		}
		entries[name] = f
		mode := f.Mode()
		if (!mode.IsRegular() && !mode.IsDir()) || mode.IsDir() != strings.HasSuffix(f.Name, "/") {
			return nil, fmt.Errorf("bundle entry %q must be a regular file or directory", name)
		}
		if mode.IsDir() && f.UncompressedSize64 != 0 {
			return nil, fmt.Errorf("bundle directory %q contains data", name)
		}
		if f.UncompressedSize64 > MaxBundleSize-total {
			return nil, errors.New("bundle expanded size exceeds 64 MiB")
		}
		total += f.UncompressedSize64
	}
	for name := range entries {
		for parent := path.Dir(name); parent != "."; parent = path.Dir(parent) {
			if f := entries[parent]; f != nil && !f.Mode().IsDir() {
				return nil, fmt.Errorf("bundle path %q has a non-directory parent", name)
			}
		}
	}
	var expanded int64
	for _, f := range zr.File {
		r, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("open bundle entry %q: %w", f.Name, err)
		}
		n, copyErr := io.Copy(io.Discard, io.LimitReader(r, MaxBundleSize-expanded+1))
		closeErr := r.Close()
		expanded += n
		if expanded > MaxBundleSize {
			return nil, errors.New("bundle expanded size exceeds 64 MiB")
		}
		if copyErr != nil {
			return nil, fmt.Errorf("read bundle entry %q: %w", f.Name, copyErr)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("close bundle entry %q: %w", f.Name, closeErr)
		}
	}
	return entries, nil
}

// ExtractArchive extracts an archive accepted by ValidateArchive into a fresh private directory below workDir.
func ExtractArchive(archive []byte, workDir string) (projectDir string, err error) {
	projectDir, err = os.MkdirTemp(workDir, "project-*")
	if err != nil {
		return "", fmt.Errorf("create bundle project: %w", err)
	}
	// Root-relative operations also prevent symlink-swap races from escaping during extraction.
	root, err := os.OpenRoot(projectDir)
	if err != nil {
		return "", err
	}
	defer root.Close()
	zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return "", err
	}
	for _, f := range zr.File {
		dest := filepath.FromSlash(f.Name)
		if f.Mode().IsDir() {
			err = root.MkdirAll(dest, 0700)
		} else {
			err = root.MkdirAll(filepath.Dir(dest), 0700)
			if err == nil {
				err = extractBundleFile(root, f, dest)
			}
		}
		if err != nil {
			return "", fmt.Errorf("extract bundle entry %q: %w", f.Name, err)
		}
	}
	return projectDir, nil
}

func extractBundleFile(root *os.Root, f *zip.File, dest string) error {
	r, err := f.Open()
	if err != nil {
		return err
	}
	defer r.Close()
	// Preserve executable scripts, but never uploaded ownership, special bits or public permissions.
	out, err := root.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600|f.Mode().Perm()&0100)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, io.LimitReader(r, MaxBundleSize+1))
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}
