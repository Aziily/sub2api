package googledrive

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAboutAndListFiles(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/about":
			if r.URL.Query().Get("fields") != "storageQuota" {
				t.Error("missing fields")
			}
			fmt.Fprint(w, `{"storageQuota":{"limit":"100","usage":"30","usageInDrive":"20"}}`)
		case "/files":
			if r.URL.Query().Get("pageToken") != "a+b/=" || r.URL.Query().Get("pageSize") != "10" {
				t.Error("pagination was not preserved")
			}
			fmt.Fprint(w, `{"files":[{"id":"1","name":"test","size":"42"}],"nextPageToken":"next"}`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	c := NewClient(server.Client())
	c.base = server.URL + "/"
	about, err := c.About(context.Background())
	if err != nil || about.StorageQuota.Limit == nil || *about.StorageQuota.Limit != 100 {
		t.Fatalf("about: %+v %v", about, err)
	}
	page, err := c.ListFiles(context.Background(), "a+b/=", 10)
	if err != nil || len(page.Files) != 1 || *page.Files[0].Size != 42 || page.NextPageToken != "next" {
		t.Fatalf("page: %+v %v", page, err)
	}
	if _, err = c.ListFiles(context.Background(), "", 101); err == nil {
		t.Fatal("invalid page size accepted")
	}
}

func TestErrorDoesNotExposeResponseBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "secret in upstream response", http.StatusForbidden)
	}))
	defer server.Close()
	c := NewClient(server.Client())
	c.base = server.URL + "/"
	_, err := c.About(context.Background())
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != 403 || err.Error() != "Google Drive API returned HTTP 403" {
		t.Fatalf("unexpected error: %v", err)
	}
}
