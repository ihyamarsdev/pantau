package web

import (
	"archive/zip"
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net/http"

	"pantau/internal/ansible"
)

var errAnsibleImportSize = errors.New("folder exceeds the 64 MiB import limit")

const maxUploadFieldBytes = 4 << 10 // plain form fields next to the uploaded files (kind, name)

// readAnsibleFolder streams a multipart body into a ZIP of its `files` parts (filename = project-relative path)
// and returns it with the plain form fields named in allowed. Streaming avoids ParseMultipartForm's default
// 1,000-part limit, below a vendored collection.
func readAnsibleFolder(r *http.Request, allowed ...string) ([]byte, map[string]string, error) {
	reader, err := r.MultipartReader()
	if err != nil {
		return nil, nil, errors.New("multipart/form-data is required")
	}
	var archive bytes.Buffer
	zw := zip.NewWriter(&archive)
	fields := make(map[string]string)
	paths := make(map[string]bool)
	var total int64
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil, err
		}
		_, params, err := mime.ParseMediaType(part.Header.Get("Content-Disposition"))
		if err != nil {
			return nil, nil, errors.New("invalid multipart content disposition")
		}
		isFile := part.FormName() == "files"
		for _, a := range allowed {
			isFile = isFile || (a == "archive" && part.FormName() == "archive")
		}
		if !isFile {
			name := part.FormName()
			known := false
			for _, a := range allowed {
				known = known || a == name
			}
			if !known {
				return nil, nil, fmt.Errorf("unexpected import field %q", name)
			}
			if _, exists := fields[name]; exists || params["filename"] != "" {
				return nil, nil, fmt.Errorf("invalid or duplicate import field %q", name)
			}
			data, err := io.ReadAll(io.LimitReader(part, maxUploadFieldBytes+1))
			if err != nil {
				return nil, nil, err
			}
			if len(data) > maxUploadFieldBytes {
				return nil, nil, fmt.Errorf("form field %q exceeds 4 KiB", name)
			}
			fields[name] = string(data)
			continue
		}
		// Part.FileName strips directories; the raw filename preserves the project's relative paths.
		name := params["filename"]
		if len(name) > 4096 {
			return nil, nil, errors.New("import file path exceeds 4096 bytes")
		}
		if err := ansible.ValidateBundleFilePath(name); err != nil {
			return nil, nil, err
		}
		if paths[name] {
			return nil, nil, fmt.Errorf("duplicate import path %q", name)
		}
		paths[name] = true
		if len(paths) > ansible.MaxBundleFiles {
			return nil, nil, fmt.Errorf("folder exceeds %d files", ansible.MaxBundleFiles)
		}
		// Browsers expose no file mode; a shebang is the only signal a script must stay executable.
		src := bufio.NewReader(io.LimitReader(part, ansible.MaxBundleSize-total+1))
		mode := fs.FileMode(0600)
		if head, _ := src.Peek(2); string(head) == "#!" {
			mode = 0700
		}
		header := &zip.FileHeader{Name: name, Method: zip.Deflate}
		header.SetMode(mode)
		file, err := zw.CreateHeader(header)
		if err != nil {
			return nil, nil, err
		}
		n, err := io.Copy(file, src)
		total += n
		if total > ansible.MaxBundleSize || archive.Len() > ansible.MaxBundleSize {
			return nil, nil, errAnsibleImportSize
		}
		if err != nil {
			return nil, nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, nil, err
	}
	if archive.Len() > ansible.MaxBundleSize {
		return nil, nil, errAnsibleImportSize
	}
	return archive.Bytes(), fields, nil
}

// readAnsibleImport applies the body cap and maps parse failures to HTTP statuses; on failure it has answered.
func readAnsibleImport(w http.ResponseWriter, r *http.Request, allowed ...string) ([]byte, map[string]string, bool) {
	// Allow multipart headers above the file-data cap, without unbounded uploads or disk spill.
	r.Body = http.MaxBytesReader(w, r.Body, ansible.MaxBundleSize+(16<<20))
	archive, fields, err := readAnsibleFolder(r, allowed...)
	if err != nil {
		status := http.StatusBadRequest
		var tooLarge *http.MaxBytesError
		if errors.Is(err, errAnsibleImportSize) || errors.As(err, &tooLarge) {
			status = http.StatusRequestEntityTooLarge
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return nil, nil, false
	}
	return archive, fields, true
}
