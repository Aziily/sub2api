package googledrive

import (
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"

	"golang.org/x/net/webdav"
)

type fakeFile struct {
	id, name, mime string
	parents        []string
	content        []byte
	trashed        bool
	version        int
}

// fakeDrive implements the subset of the Drive v3 API used by Client.
type fakeDrive struct {
	mu    sync.Mutex
	files map[string]*fakeFile
	next  int
}

var parentQuery = regexp.MustCompile(`^trashed = false and '([A-Za-z0-9_-]+)' in parents$`)

func (d *fakeDrive) add(name, mimeType, parent string, content []byte) *fakeFile {
	d.next++
	f := &fakeFile{id: fmt.Sprintf("f%d", d.next), name: name, mime: mimeType, parents: []string{parent}, content: content, version: 1}
	d.files[f.id] = f
	return f
}

func (f *fakeFile) json() map[string]any {
	m := map[string]any{"id": f.id, "name": f.name, "mimeType": f.mime, "parents": f.parents, "modifiedTime": fmt.Sprintf("2026-10-29T00:00:%02dZ", f.version%60)}
	if f.mime != FolderMimeType {
		m["size"] = fmt.Sprint(len(f.content))
	}
	return m
}

func (d *fakeDrive) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	d.mu.Lock()
	defer d.mu.Unlock()
	q := r.URL.Query()
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/drive/v3/files":
		m := parentQuery.FindStringSubmatch(q.Get("q"))
		if m == nil {
			http.Error(w, "bad query", 400)
			return
		}
		var out []map[string]any
		for _, f := range d.files {
			if !f.trashed && f.parents[0] == m[1] {
				out = append(out, f.json())
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"files": out})
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/drive/v3/files/"):
		f := d.files[strings.TrimPrefix(r.URL.Path, "/drive/v3/files/")]
		if f == nil {
			http.NotFound(w, r)
			return
		}
		if q.Get("alt") == "media" {
			_, _ = w.Write(f.content)
			return
		}
		_ = json.NewEncoder(w).Encode(f.json())
	case r.Method == http.MethodPost && r.URL.Path == "/drive/v3/files":
		var meta struct {
			Name     string   `json:"name"`
			MimeType string   `json:"mimeType"`
			Parents  []string `json:"parents"`
		}
		_ = json.NewDecoder(r.Body).Decode(&meta)
		_ = json.NewEncoder(w).Encode(d.add(meta.Name, meta.MimeType, meta.Parents[0], nil).json())
	case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/drive/v3/files/"):
		f := d.files[strings.TrimPrefix(r.URL.Path, "/drive/v3/files/")]
		if f == nil {
			http.NotFound(w, r)
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if t, ok := body["trashed"].(bool); ok && t {
			f.trashed = true
		} else {
			if name, ok := body["name"].(string); ok {
				f.name = name
			}
			if add := q.Get("addParents"); add != "" {
				if f.parents[0] != q.Get("removeParents") {
					http.Error(w, "wrong parent", 400)
					return
				}
				f.parents = []string{add}
			}
			f.version++
		}
		_ = json.NewEncoder(w).Encode(f.json())
	case r.Method == http.MethodPost && r.URL.Path == "/upload/drive/v3/files":
		_, params, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
		mr := multipart.NewReader(r.Body, params["boundary"])
		part, _ := mr.NextPart()
		var meta struct {
			Name    string   `json:"name"`
			Parents []string `json:"parents"`
		}
		_ = json.NewDecoder(part).Decode(&meta)
		part, _ = mr.NextPart()
		data, _ := io.ReadAll(part)
		_ = json.NewEncoder(w).Encode(d.add(meta.Name, part.Header.Get("Content-Type"), meta.Parents[0], data).json())
	case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/upload/drive/v3/files/"):
		f := d.files[strings.TrimPrefix(r.URL.Path, "/upload/drive/v3/files/")]
		if f == nil {
			http.NotFound(w, r)
			return
		}
		f.content, _ = io.ReadAll(r.Body)
		f.version++
		_ = json.NewEncoder(w).Encode(f.json())
	default:
		http.Error(w, "unexpected "+r.Method+" "+r.URL.Path, 500)
	}
}

func TestWebDAVOverDriveStaysInRoot(t *testing.T) {
	drive := &fakeDrive{files: map[string]*fakeFile{}}
	root := drive.add("Sub2API - user", FolderMimeType, "driveroot", nil)
	other := drive.add("Sub2API - other", FolderMimeType, "driveroot", nil)
	secret := drive.add("secret.txt", "text/plain", other.id, []byte("other user"))
	api := httptest.NewServer(drive)
	defer api.Close()
	c := NewClient(api.Client())
	c.base = api.URL + "/drive/v3/"

	dav := httptest.NewServer(&webdav.Handler{Prefix: "/dav", FileSystem: NewFS(c, root.id, 16), LockSystem: webdav.NewMemLS()})
	defer dav.Close()
	do := func(method, path, body string, headers ...string) (int, string) {
		t.Helper()
		req, _ := http.NewRequest(method, dav.URL+path, strings.NewReader(body))
		for i := 0; i+1 < len(headers); i += 2 {
			req.Header.Set(headers[i], headers[i+1])
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		data, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(data)
	}
	expect := func(got, want int, what string) {
		t.Helper()
		if got != want {
			t.Fatalf("%s: got %d, want %d", what, got, want)
		}
	}

	st, _ := do("MKCOL", "/dav/docs", "")
	expect(st, 201, "mkcol")
	st, _ = do("PUT", "/dav/docs/a.txt", "hello")
	expect(st, 201, "create")
	st, body := do("GET", "/dav/docs/a.txt", "")
	if st != 200 || body != "hello" {
		t.Fatalf("read: %d %q", st, body)
	}
	st, _ = do("PUT", "/dav/docs/a.txt", "world")
	// x/net/webdav reports 201 for every successful PUT; the copy count
	// below verifies the existing file was replaced in place.
	expect(st, 201, "overwrite")
	count := 0
	for _, f := range drive.files {
		if f.name == "a.txt" && !f.trashed {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("overwrite created %d copies", count)
	}
	st, body = do("PROPFIND", "/dav/docs/", "", "Depth", "1")
	if st != 207 || !strings.Contains(body, "a.txt") {
		t.Fatalf("propfind: %d", st)
	}
	st, body = do("PROPFIND", "/dav/", "", "Depth", "1")
	if st != 207 || strings.Contains(body, "secret") || strings.Contains(body, "Sub2API - other") {
		t.Fatalf("root listing leaked other entries: %d", st)
	}
	st, _ = do("MOVE", "/dav/docs/a.txt", "", "Destination", dav.URL+"/dav/docs/b.txt")
	expect(st, 201, "move")
	st, body = do("GET", "/dav/docs/b.txt", "")
	if st != 200 || body != "world" {
		t.Fatalf("read moved: %d %q", st, body)
	}
	st, _ = do("GET", "/dav/docs/a.txt", "")
	expect(st, 404, "old name")
	st, _ = do("DELETE", "/dav/docs/b.txt", "")
	expect(st, 204, "delete")
	st, _ = do("GET", "/dav/docs/b.txt", "")
	expect(st, 404, "deleted")
	st, _ = do("PUT", "/dav/big.bin", strings.Repeat("x", 17))
	if st < 400 {
		t.Fatalf("oversized upload accepted: %d", st)
	}
	st, _ = do("GET", "/dav/../secret.txt", "")
	if st == 200 {
		t.Fatal("traversal reached another folder")
	}
	st, _ = do("DELETE", "/dav/", "")
	if st < 400 {
		t.Fatal("root deletion accepted")
	}
	if secret.trashed || string(secret.content) != "other user" {
		t.Fatal("other user's file changed")
	}
}
