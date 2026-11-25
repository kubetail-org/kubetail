package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

const fixtureFile = `{"document": {"children": [
	{"id": "0:1", "name": "Cover"},
	{"id": "3:3", "name": "Components"},
	{"id": "3:4", "name": "Screens"}
]}}`

// figmaServer serves the file's page list and testdata/nodes.json as Figma's
// API does, to a client with the token.
func figmaServer(t *testing.T, fileBody string, nodes []byte) figmaFile {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Figma-Token") != "figd_test" {
			http.Error(w, `{"status":403,"err":"Invalid token"}`, http.StatusForbidden)
			return
		}
		q := r.URL.Query()
		switch {
		case r.URL.Path == "/files/KEY" && q.Get("depth") == "1":
			w.Write([]byte(fileBody))
		case r.URL.Path == "/files/KEY/nodes" && q.Get("ids") == "3:3,3:4" && q.Get("geometry") == "paths":
			w.Write(nodes)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return figmaFile{api: srv.URL, key: "KEY", token: "figd_test", client: srv.Client()}
}

func TestFigmaFileReadsTheComponentsAndScreensPages(t *testing.T) {
	got, err := figmaServer(t, fixtureFile, fixtureNodes(t)).read()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 6 || got[0].Node != "9:148" || got[5].Node != "43:2" {
		t.Errorf("read = %+v", got)
	}
}

func TestFigmaFileReportsWhatItCannotRead(t *testing.T) {
	t.Run("refused", func(t *testing.T) {
		f := figmaServer(t, fixtureFile, fixtureNodes(t))
		f.token = "figd_wrong"
		_, err := f.read()
		wantErr(t, err, `403 Forbidden: {"status":403,"err":"Invalid token"}`)
	})
	t.Run("no Screens page", func(t *testing.T) {
		_, err := figmaServer(t, `{"document": {"children": [{"id": "3:3", "name": "Components"}]}}`, nil).read()
		wantErr(t, err, "has no Components page or no Screens page")
	})
	t.Run("a page missing from the nodes", func(t *testing.T) {
		_, err := figmaServer(t, fixtureFile, []byte(`{"nodes": {"3:3": null}}`)).read()
		wantErr(t, err, "returned no Components page or no Screens page")
	})
	t.Run("not JSON", func(t *testing.T) {
		_, err := figmaServer(t, fixtureFile, []byte(`<html>`)).read()
		wantErr(t, err, "GET /files/KEY/nodes: invalid character")
	})
	t.Run("unreachable", func(t *testing.T) {
		f := figmaServer(t, fixtureFile, nil)
		f.api = "http://127.0.0.1:0"
		_, err := f.read()
		wantErr(t, err, `Get "http://127.0.0.1:0/files/KEY?depth=1"`)
	})
	t.Run("a bad URL", func(t *testing.T) {
		f := figmaServer(t, fixtureFile, nil)
		f.api = "http://bad host"
		_, err := f.read()
		wantErr(t, err, "invalid character")
	})
}
