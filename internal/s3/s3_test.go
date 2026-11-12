package s3

import (
	"bytes"
	"context"
	"encoding/xml"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// Signing example "GET Object" from the AWS SigV4 S3 documentation (bucket examplebucket, key test.txt, Range header).
func TestSignMatchesAWSDocumentationVector(t *testing.T) {
	c := New(Config{Region: "us-east-1", Bucket: "examplebucket", AccessKey: "AKIAIOSFODNN7EXAMPLE", SecretKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"})
	req, _ := http.NewRequest(http.MethodGet, "https://examplebucket.s3.amazonaws.com/test.txt", nil)
	req.Header.Set("Range", "bytes=0-9")
	c.sign(req, emptySHA256, time.Date(2013, 5, 24, 0, 0, 0, 0, time.UTC))

	want := "AWS4-HMAC-SHA256 Credential=AKIAIOSFODNN7EXAMPLE/20130524/us-east-1/s3/aws4_request, " +
		"SignedHeaders=host;range;x-amz-content-sha256;x-amz-date, " +
		"Signature=f0e8bdb87c964420e857bd35b5d6ed310bd44f0170aba48dd91039c6036bdb41"
	if got := req.Header.Get("Authorization"); got != want {
		t.Fatalf("signature mismatch\n got: %s\nwant: %s", got, want)
	}
}

func TestObjectURLAddressingAndEncoding(t *testing.T) {
	cases := []struct {
		cfg  Config
		key  string
		want string
	}{
		{Config{Region: "eu-west-1", Bucket: "b"}, "a b/é+.tar.gz", "https://b.s3.eu-west-1.amazonaws.com/a%20b/%C3%A9%2B.tar.gz"},
		{Config{Endpoint: "http://minio:9000", Bucket: "b", PathStyle: true}, "k/x", "http://minio:9000/b/k/x"},
		{Config{Endpoint: "https://s3.example.com/base", Bucket: "b"}, "k", "https://b.s3.example.com/base/k"},
	}
	for _, c := range cases {
		got, err := New(c.cfg).objectURL(c.key, nil)
		if err != nil || got != c.want {
			t.Errorf("objectURL(%q) = %q, %v; want %q", c.key, got, err, c.want)
		}
	}
}

func TestNormalizePrefix(t *testing.T) {
	for in, want := range map[string]string{"": "", "/": "", "a": "a/", "/a//b/": "a/b/", "../../x": "x/", `a\b`: "a/b/"} {
		if got := NormalizePrefix(in); got != want {
			t.Errorf("NormalizePrefix(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestValidate(t *testing.T) {
	ok := Config{Bucket: "my-bucket", AccessKey: "a", SecretKey: "s"}
	if err := ok.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]Config{
		"missing secret": {Bucket: "b1", AccessKey: "a"},
		"bad bucket":     {Bucket: "bad/bucket", AccessKey: "a", SecretKey: "s"},
		"bad endpoint":   {Bucket: "b1", AccessKey: "a", SecretKey: "s", Endpoint: "ftp://x"},
	} {
		if c.Validate() == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

// fakeS3 records requests and implements just enough of PUT / multipart for Upload.
type fakeS3 struct {
	mu       sync.Mutex
	puts     map[string][]byte
	parts    map[string][]byte // partNumber -> body
	aborted  bool
	complete []string
	failPart string
}

func (f *fakeS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	body, _ := io.ReadAll(r.Body)
	q := r.URL.Query()
	if !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 ") {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	switch {
	case r.Method == http.MethodPost && q.Has("uploads"):
		_, _ = w.Write([]byte(`<InitiateMultipartUploadResult><UploadId>up1</UploadId></InitiateMultipartUploadResult>`))
	case r.Method == http.MethodPut && q.Get("uploadId") != "":
		if q.Get("partNumber") == f.failPart {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`<Error><Code>InternalError</Code><Message>boom</Message></Error>`))
			return
		}
		f.parts[q.Get("partNumber")] = body
		w.Header().Set("ETag", `"etag`+q.Get("partNumber")+`"`)
	case r.Method == http.MethodPost && q.Get("uploadId") != "":
		var c struct {
			Parts []struct{ PartNumber int } `xml:"Part"`
		}
		_ = xml.Unmarshal(body, &c)
		for _, p := range c.Parts {
			f.complete = append(f.complete, string(f.parts[strconv.Itoa(p.PartNumber)]))
		}
		_, _ = w.Write([]byte(`<CompleteMultipartUploadResult/>`))
	case r.Method == http.MethodDelete && q.Get("uploadId") != "":
		f.aborted = true
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodPut:
		f.puts[r.URL.Path] = body
	default:
		w.WriteHeader(http.StatusBadRequest)
	}
}

func newFake(t *testing.T) (*Client, *fakeS3) {
	f := &fakeS3{puts: map[string][]byte{}, parts: map[string][]byte{}}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return New(Config{Endpoint: srv.URL, Region: "us-east-1", Bucket: "bkt", AccessKey: "a", SecretKey: "s", PathStyle: true}), f
}

func TestUploadSmallObjectIsSinglePut(t *testing.T) {
	c, f := newFake(t)
	if err := c.Upload(context.Background(), "dir/a.txt", strings.NewReader("hello"), 5); err != nil {
		t.Fatal(err)
	}
	if string(f.puts["/bkt/dir/a.txt"]) != "hello" || len(f.parts) != 0 {
		t.Fatalf("expected single PUT, got puts=%v parts=%d", f.puts, len(f.parts))
	}
}

func TestUploadMultipartReassemblesInOrder(t *testing.T) {
	// 3 full parts + a 5-byte tail, and an exact multiple of the part size, must both complete without losing bytes.
	for _, tail := range []int{5, 0} {
		c, f := newFake(t)
		data := append(bytes.Repeat([]byte("x"), minPartSize), bytes.Repeat([]byte("y"), minPartSize)...)
		data = append(data, bytes.Repeat([]byte("z"), tail)...)
		if err := c.Upload(context.Background(), "big", bytes.NewReader(data), int64(len(data))); err != nil {
			t.Fatalf("tail=%d: %v", tail, err)
		}
		if got := strings.Join(f.complete, ""); got != string(data) {
			t.Fatalf("tail=%d: reassembled %d bytes, want %d", tail, len(got), len(data))
		}
	}
}

func TestUploadAbortsMultipartOnPartFailure(t *testing.T) {
	c, f := newFake(t)
	f.failPart = "2"
	data := bytes.Repeat([]byte("x"), 2*minPartSize+1)
	err := c.Upload(context.Background(), "big", bytes.NewReader(data), int64(len(data)))
	if err == nil || !strings.Contains(err.Error(), "InternalError") {
		t.Fatalf("expected InternalError, got %v", err)
	}
	if !f.aborted {
		t.Fatal("failed multipart upload must be aborted so S3 does not keep billing for the parts")
	}
}
