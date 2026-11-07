package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"pantau/internal/store"
)

const linkTestState = `{"version":4,"serial":1,"lineage":"l","outputs":{
 "vpc":{"value":"vpc-1","type":"string"},
 "tags":{"value":{"a":"b","n":[1,2]},"type":["object",{}]},
 "pw":{"value":"s3cret","type":"string","sensitive":true}},"resources":[]}`

func linkTestWS(t *testing.T, srv *Server, name string, withState bool) int64 {
	t.Helper()
	id, err := srv.db.CreateTFWorkspace(&store.TFWorkspace{Name: name, Source: "inline"})
	if err != nil {
		t.Fatal(err)
	}
	if withState {
		if _, err := srv.db.CreateTFStateVersion(&store.TFStateVersion{WorkspaceID: id, State: []byte(linkTestState)}); err != nil {
			t.Fatal(err)
		}
	}
	return id
}

func linkReq(srv *Server, method string, id int64, body any) *httptest.ResponseRecorder {
	var rd *strings.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = strings.NewReader(string(b))
	} else {
		rd = strings.NewReader("")
	}
	req := httptest.NewRequest(method, fmt.Sprintf("/api/terraform/workspaces/%d/output-links", id), rd)
	rec := httptest.NewRecorder()
	srv.handleTerraformWorkspaceRoute(rec, req)
	return rec
}

type linkItem = map[string]any

func putLinks(srv *Server, id int64, links ...linkItem) *httptest.ResponseRecorder {
	return linkReq(srv, http.MethodPut, id, map[string]any{"links": links})
}

func lk(v string, src int64, out string) linkItem {
	return linkItem{"var_name": v, "source_workspace_id": src, "output_name": out}
}

func TestOutputLinkHandlerValidation(t *testing.T) {
	srv, _ := setupTerraformTestDataServer(t)
	a := linkTestWS(t, srv, "a", false)
	b := linkTestWS(t, srv, "b", true)
	c := linkTestWS(t, srv, "c", false)
	d := linkTestWS(t, srv, "d", false)

	for name, l := range map[string]linkItem{
		"self":     lk("x", a, "vpc"),
		"missing":  lk("x", 9999, "vpc"),
		"badvar":   lk("1x", b, "vpc"),
		"badvar2":  lk("x-y", b, "vpc"),
		"noOutput": lk("x", b, ""),
	} {
		if rec := putLinks(srv, a, l); rec.Code != 400 || !strings.Contains(rec.Body.String(), `"error"`) {
			t.Errorf("%s: code %d %s", name, rec.Code, rec.Body)
		}
	}
	if rec := putLinks(srv, a, lk("x", b, "vpc"), lk("x", c, "vpc")); rec.Code != 400 {
		t.Errorf("duplicate var: %d", rec.Code)
	}

	// cycles: a<-b, then b<-a is a 2-cycle; chain b<-c<-d then d<-b is a longer one
	if rec := putLinks(srv, a, lk("x", b, "vpc")); rec.Code != 200 {
		t.Fatalf("a<-b: %d %s", rec.Code, rec.Body)
	}
	if rec := putLinks(srv, b, lk("x", a, "vpc")); rec.Code != 400 {
		t.Errorf("A->B->A accepted: %d", rec.Code)
	}
	if rec := putLinks(srv, c, lk("x", d, "o")); rec.Code != 200 {
		t.Fatalf("c<-d: %d", rec.Code)
	}
	if rec := putLinks(srv, b, lk("x", c, "o")); rec.Code != 200 { // a<-b<-c<-d
		t.Fatalf("b<-c: %d %s", rec.Code, rec.Body)
	}
	if rec := putLinks(srv, d, lk("x", a, "o")); rec.Code != 400 {
		t.Errorf("long cycle accepted: %d", rec.Code)
	}
	// re-saving the same set is not a cycle with itself
	if rec := putLinks(srv, a, lk("x", b, "vpc")); rec.Code != 200 {
		t.Errorf("idempotent PUT: %d", rec.Code)
	}
	// empty list clears
	if rec := putLinks(srv, a); rec.Code != 200 {
		t.Errorf("clear: %d", rec.Code)
	}
	if rec := linkReq(srv, http.MethodDelete, a, nil); rec.Code != 405 {
		t.Errorf("DELETE: %d", rec.Code)
	}
}

func TestOutputLinkHandlerGet(t *testing.T) {
	srv, _ := setupTerraformTestDataServer(t)
	a := linkTestWS(t, srv, "a", true)
	b := linkTestWS(t, srv, "b", true)
	linkTestWS(t, srv, "nostate", false)
	if rec := putLinks(srv, a, lk("net", b, "vpc"), lk("gone", b, "missing")); rec.Code != 200 {
		t.Fatalf("put: %d %s", rec.Code, rec.Body)
	}
	rec := linkReq(srv, http.MethodGet, a, nil)
	var out struct {
		Links []struct {
			VarName    string `json:"var_name"`
			SrcName    string `json:"source_workspace_name"`
			OutputName string `json:"output_name"`
			Available  bool   `json:"available"`
		} `json:"links"`
		Sources []struct {
			ID      int64  `json:"id"`
			Name    string `json:"name"`
			Outputs []struct {
				Name      string `json:"name"`
				Sensitive bool   `json:"sensitive"`
			} `json:"outputs"`
		} `json:"sources"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || rec.Code != 200 {
		t.Fatalf("%d %v %s", rec.Code, err, rec.Body)
	}
	if len(out.Links) != 2 || out.Links[0].SrcName != "b" || !out.Links[0].Available || out.Links[1].Available {
		t.Errorf("links = %+v", out.Links)
	}
	if len(out.Sources) != 1 || out.Sources[0].ID != b || len(out.Sources[0].Outputs) != 3 {
		t.Fatalf("sources = %+v", out.Sources)
	}
	if o := out.Sources[0].Outputs; o[0].Name != "pw" || !o[0].Sensitive || o[1].Name != "tags" || o[2].Name != "vpc" {
		t.Errorf("outputs = %+v", o)
	}
	if strings.Contains(rec.Body.String(), "s3cret") {
		t.Error("sensitive value leaked")
	}
}
