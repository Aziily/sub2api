package googledrive

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCreateUploadSession(t *testing.T) {
	location := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var meta map[string]any
		_ = json.NewDecoder(r.Body).Decode(&meta)
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/upload/drive/v3/files":
			if parents, _ := meta["parents"].([]any); len(parents) != 1 || parents[0] != "folder1" {
				t.Errorf("new file not placed in parent: %v", meta)
			}
			if props, _ := meta["appProperties"].(map[string]any); props["node"] != "n42" {
				t.Errorf("application tag missing: %v", meta)
			}
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		if r.URL.Query().Get("uploadType") != "resumable" || r.Header.Get("X-Upload-Content-Length") != "5" || r.Header.Get("Origin") != "https://drive.example" {
			t.Errorf("missing resumable headers: %v %v", r.URL.Query(), r.Header)
		}
		w.Header().Set("Location", location)
	}))
	defer server.Close()
	c := NewClient(server.Client())
	c.base = server.URL + "/drive/v3/"
	req := UploadSessionRequest{Name: "a.txt", ParentID: "folder1", Size: 5, Origin: "https://drive.example", AppProperties: map[string]string{"node": "n42"}}

	location = server.URL + "/upload/drive/v3/files?upload_id=abc"
	if got, err := c.CreateUploadSession(context.Background(), req); err != nil || got != location {
		t.Fatalf("create: %q %v", got, err)
	}
	// A session URI is a bearer capability; never relay one for another host.
	location = "https://attacker.example/upload/drive/v3/files?upload_id=abc"
	if _, err := c.CreateUploadSession(context.Background(), req); err == nil {
		t.Fatal("foreign upload location accepted")
	}
}
