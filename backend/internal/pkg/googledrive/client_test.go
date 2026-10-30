package googledrive

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAboutAndListFiles(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/about":
			if r.URL.Query().Get("fields") != "storageQuota,user(emailAddress,displayName)" {
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

func TestListChildrenRestrictsQueryToParent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("q") != "trashed = false and 'folder_123' in parents" || r.URL.Query().Get("pageToken") != "next" {
			t.Error("unexpected folder query")
		}
		fmt.Fprint(w, `{"files":[{"id":"child","name":"file","parents":["folder_123"]}]}`)
	}))
	defer server.Close()
	c := NewClient(server.Client())
	c.base = server.URL + "/"
	page, err := c.ListChildren(context.Background(), "folder_123", "next", 10)
	if err != nil || len(page.Files) != 1 || page.Files[0].Parents[0] != "folder_123" {
		t.Fatal("folder listing failed")
	}
	if _, err := c.ListChildren(context.Background(), "x' or trashed = false", "", 10); err == nil {
		t.Fatal("accepted query injection")
	}
}

func TestCreateAndTrashFolder(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/files" {
			var meta struct {
				Name     string   `json:"name"`
				MimeType string   `json:"mimeType"`
				Parents  []string `json:"parents"`
			}
			if err := json.NewDecoder(r.Body).Decode(&meta); err != nil || meta.Name != "folder" || meta.MimeType != "application/vnd.google-apps.folder" || len(meta.Parents) != 1 || meta.Parents[0] != "root" {
				t.Error("invalid folder metadata")
			}
			fmt.Fprint(w, `{"id":"folder1","name":"folder"}`)
			return
		}
		if r.Method == http.MethodPatch && r.URL.Path == "/files/folder1" {
			var data map[string]bool
			if err := json.NewDecoder(r.Body).Decode(&data); err != nil || !data["trashed"] {
				t.Error("file was not trashed")
			}
			fmt.Fprint(w, `{}`)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()
	c := NewClient(server.Client())
	c.base = server.URL + "/"
	folder, err := c.CreateFolder(context.Background(), "folder", "root")
	if err != nil || folder.ID != "folder1" {
		t.Fatal("folder creation failed")
	}
	if err := c.Trash(context.Background(), folder.ID); err != nil {
		t.Fatal(err)
	}
}

func TestReplaceContentAndMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPatch && r.URL.Path == "/upload/drive/v3/files/file1":
			content, _ := io.ReadAll(r.Body)
			if string(content) != "replacement" || r.URL.Query().Get("uploadType") != "media" {
				t.Error("invalid replacement")
			}
			fmt.Fprint(w, `{"id":"file1","size":"11"}`)
		case r.Method == http.MethodGet && r.URL.Path == "/drive/v3/files/file1":
			fmt.Fprint(w, `{"id":"file1","parents":["root"]}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	c := NewClient(server.Client())
	c.base = server.URL + "/drive/v3/"
	f, err := c.ReplaceContent(context.Background(), "file1", "text/plain", strings.NewReader("replacement"))
	if err != nil || f.ID != "file1" {
		t.Fatal("replacement failed")
	}
	f, err = c.GetFile(context.Background(), "file1")
	if err != nil || len(f.Parents) != 1 || f.Parents[0] != "root" {
		t.Fatal("metadata failed")
	}
}

func TestUploadAndDownload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/upload/drive/v3/files":
			if r.URL.Query().Get("uploadType") != "multipart" {
				t.Error("wrong upload type")
			}
			_, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
			if err != nil {
				t.Fatal(err)
			}
			mr := multipart.NewReader(r.Body, params["boundary"])
			first, err := mr.NextPart()
			if err != nil {
				t.Fatal(err)
			}
			var meta struct {
				Name    string   `json:"name"`
				Parents []string `json:"parents"`
			}
			if err := json.NewDecoder(first).Decode(&meta); err != nil || meta.Name != "test.txt" || len(meta.Parents) != 1 || meta.Parents[0] != "folder" {
				t.Fatalf("metadata: %+v %v", meta, err)
			}
			part, err := mr.NextPart()
			if err != nil {
				t.Fatal(err)
			}
			data, err := io.ReadAll(part)
			if err != nil || string(data) != "hello" {
				t.Fatalf("content %q: %v", data, err)
			}
			fmt.Fprint(w, `{"id":"file1","name":"test.txt","size":"5"}`)
		case "/drive/v3/files/file1":
			if r.URL.Query().Get("alt") != "media" {
				t.Error("missing alt=media")
			}
			fmt.Fprint(w, "hello")
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	c := NewClient(server.Client())
	c.base = server.URL + "/drive/v3/"
	file, err := c.Upload(context.Background(), "test.txt", "folder", "text/plain", strings.NewReader("hello"))
	if err != nil || file.ID != "file1" {
		t.Fatalf("upload: %+v %v", file, err)
	}
	body, err := c.Download(context.Background(), file.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer body.Close()
	data, err := io.ReadAll(body)
	if err != nil || string(data) != "hello" {
		t.Fatalf("download: %q %v", data, err)
	}
	if _, err := c.Download(context.Background(), "../other"); err == nil {
		t.Fatal("invalid ID accepted")
	}
}
