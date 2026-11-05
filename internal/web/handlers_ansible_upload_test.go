package web

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"

	"pantau/internal/ansible"
)

func TestAnsibleUploadLimits(t *testing.T) {
	t.Run("file count", func(t *testing.T) {
		files := make([]ansibleImportFile, ansible.MaxBundleFiles+1)
		for i := range files {
			files[i] = ansibleImportFile{fmt.Sprintf("files/%d", i), ""}
		}
		req := ansibleFolderRequest(t, "/api/ansible/library/local", nil, files)
		if _, _, err := readAnsibleFolder(req); err == nil || !strings.Contains(err.Error(), "10000 files") {
			t.Fatalf("file-count limit not enforced: %v", err)
		}
	})
	t.Run("combined bytes", func(t *testing.T) {
		reader, pipe := io.Pipe()
		writer := multipart.NewWriter(pipe)
		done := make(chan struct{})
		go func() {
			defer close(done)
			defer pipe.Close()
			part, err := writer.CreateFormFile("files", "small")
			if err != nil {
				return
			}
			if _, err := io.WriteString(part, "x"); err != nil {
				return
			}
			part, err = writer.CreateFormFile("files", "files/large")
			if err != nil {
				return
			}
			chunk := bytes.Repeat([]byte("x"), 1<<20)
			for n := 0; n <= ansible.MaxBundleSize; n += len(chunk) {
				if _, err := part.Write(chunk); err != nil {
					return
				}
			}
			writer.Close()
		}()
		req := httptest.NewRequest(http.MethodPost, "/api/ansible/library/local", reader)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		_, _, err := readAnsibleFolder(req)
		reader.Close()
		<-done
		if !errors.Is(err, errAnsibleImportSize) {
			t.Fatalf("combined file-size limit not enforced: %v", err)
		}
	})
	t.Run("oversized field and unknown field", func(t *testing.T) {
		big := ansibleFolderRequest(t, "/x", map[string]string{"name": strings.Repeat("a", maxUploadFieldBytes+1)}, nil)
		if _, _, err := readAnsibleFolder(big, "name"); err == nil {
			t.Error("oversized form field accepted")
		}
		unknown := ansibleFolderRequest(t, "/x", map[string]string{"surprise": "1"}, nil)
		if _, _, err := readAnsibleFolder(unknown, "name"); err == nil {
			t.Error("unknown form field accepted")
		}
	})
}

func TestAnsibleUploadNativeMultipart(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is required for native FormData serialization")
	}
	script := `
(async () => {
  const data = new FormData();
  data.append('kind', 'role');
  data.append('files', new Blob(['- hosts: all\n']), 'tasks/deploy%22prod.yml');
  data.append('files', new Blob(['- debug: msg=hello\n']), 'defaults/main.yml');
  const request = new Request('http://example.test/api/ansible/library/local', {method: 'POST', body: data});
  console.log(JSON.stringify({contentType: request.headers.get('content-type'), body: Buffer.from(await request.arrayBuffer()).toString('base64')}));
})().catch(error => {console.error(error); process.exitCode = 1;});`
	out, err := exec.Command(node, "-e", script).Output()
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		ContentType string `json:"contentType"`
		Body        []byte `json:"body"`
	}
	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/ansible/library/local", bytes.NewReader(payload.Body))
	req.Header.Set("Content-Type", payload.ContentType)
	archive, fields, err := readAnsibleFolder(req, "kind")
	if err != nil {
		t.Fatal(err)
	}
	if fields["kind"] != "role" {
		t.Fatalf("fields: %v", fields)
	}
	zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"tasks/deploy%22prod.yml", "defaults/main.yml"} {
		if _, err := zr.Open(name); err != nil {
			t.Fatalf("native FormData lost %s (literal percent sequences must not be decoded): %v", name, err)
		}
	}
}
