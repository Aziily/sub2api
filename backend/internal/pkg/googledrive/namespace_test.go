package googledrive

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestResolvePathStaysWithinRootAndRejectsAmbiguousNames(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("q") {
		case "trashed = false and 'root42' in parents":
			fmt.Fprint(w, `{"files":[{"id":"folder42","name":"docs","mimeType":"application/vnd.google-apps.folder"}]}`)
		case "trashed = false and 'folder42' in parents":
			fmt.Fprint(w, `{"files":[{"id":"a","name":"same"},{"id":"b","name":"same"},{"id":"c","name":"one"}]}`)
		default:
			t.Error("query escaped configured root")
		}
	}))
	defer server.Close()
	c := NewClient(server.Client())
	c.base = server.URL + "/"
	f, err := c.ResolvePath(context.Background(), "root42", "/docs/one")
	if err != nil || f.ID != "c" {
		t.Fatal("path resolution failed")
	}
	if _, err := c.ResolvePath(context.Background(), "root42", "/docs/same"); !errors.Is(err, ErrAmbiguousPath) {
		t.Fatal("duplicate name accepted")
	}
	if _, err := c.ResolvePath(context.Background(), "root42", "/../other"); err == nil {
		t.Fatal("path traversal accepted")
	}
}
