package googledrive

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"os"
	"path"
	"strings"
	"time"

	"golang.org/x/net/webdav"
)

// ErrTooLarge is returned when an upload exceeds the configured size limit.
var ErrTooLarge = errors.New("file exceeds the configured size limit")

const nativeDocPrefix = "application/vnd.google-apps."

// FS exposes a single Drive folder as a webdav.FileSystem. Every path is
// resolved from rootID, so a user can never address files outside it.
type FS struct {
	c        *Client
	root     string
	maxBytes int64
}

func NewFS(c *Client, rootID string, maxBytes int64) *FS {
	return &FS{c: c, root: rootID, maxBytes: maxBytes}
}

func clean(name string) string { return path.Clean("/" + name) }

func mapErr(err error) error {
	if errors.Is(err, ErrPathNotFound) {
		return os.ErrNotExist
	}
	return err
}

func (f *FS) resolve(ctx context.Context, name string) (*File, error) {
	file, err := f.c.ResolvePath(ctx, f.root, clean(name))
	return file, mapErr(err)
}

// parentDir resolves the folder that should contain name.
func (f *FS) parentDir(ctx context.Context, name string) (*File, string, error) {
	p := clean(name)
	if p == "/" {
		return nil, "", os.ErrInvalid
	}
	dir, base := path.Split(p)
	parent, err := f.resolve(ctx, dir)
	if err != nil {
		return nil, "", err
	}
	if parent.MimeType != FolderMimeType {
		return nil, "", os.ErrNotExist
	}
	return parent, base, nil
}

func (f *FS) Mkdir(ctx context.Context, name string, _ os.FileMode) error {
	parent, base, err := f.parentDir(ctx, name)
	if err != nil {
		if errors.Is(err, os.ErrInvalid) {
			return os.ErrExist
		}
		return err
	}
	if _, err := f.resolve(ctx, name); err == nil {
		return os.ErrExist
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	_, err = f.c.CreateFolder(ctx, base, parent.ID)
	return err
}

func (f *FS) Stat(ctx context.Context, name string) (os.FileInfo, error) {
	file, err := f.resolve(ctx, name)
	if err != nil {
		return nil, err
	}
	return newInfo(file, clean(name)), nil
}

func (f *FS) RemoveAll(ctx context.Context, name string) error {
	if clean(name) == "/" {
		return os.ErrPermission
	}
	file, err := f.resolve(ctx, name)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	// Trash is recoverable from Drive, unlike permanent deletion.
	return f.c.Trash(ctx, file.ID)
}

func (f *FS) Rename(ctx context.Context, oldName, newName string) error {
	if clean(oldName) == "/" || clean(newName) == "/" {
		return os.ErrPermission
	}
	file, err := f.resolve(ctx, oldName)
	if err != nil {
		return err
	}
	oldParent, _, err := f.parentDir(ctx, oldName)
	if err != nil {
		return err
	}
	newParent, base, err := f.parentDir(ctx, newName)
	if err != nil {
		return err
	}
	if _, err := f.resolve(ctx, newName); err == nil {
		return os.ErrExist
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	_, err = f.c.Move(ctx, file.ID, base, oldParent.ID, newParent.ID)
	return err
}

func (f *FS) OpenFile(ctx context.Context, name string, flag int, _ os.FileMode) (webdav.File, error) {
	if flag&(os.O_WRONLY|os.O_RDWR|os.O_CREATE|os.O_TRUNC|os.O_APPEND) != 0 {
		return f.openWrite(ctx, name, flag)
	}
	file, err := f.resolve(ctx, name)
	if err != nil {
		return nil, err
	}
	info := newInfo(file, clean(name))
	if file.MimeType == FolderMimeType {
		return &dirFile{ctx: ctx, c: f.c, folderID: file.ID, info: info}, nil
	}
	return &readFile{ctx: ctx, c: f.c, file: file, info: info, maxBytes: f.maxBytes}, nil
}

func (f *FS) openWrite(ctx context.Context, name string, flag int) (webdav.File, error) {
	if flag&os.O_APPEND != 0 {
		return nil, os.ErrInvalid
	}
	parent, base, err := f.parentDir(ctx, name)
	if err != nil {
		return nil, err
	}
	existing, err := f.resolve(ctx, name)
	switch {
	case err == nil:
		if flag&os.O_EXCL != 0 {
			return nil, os.ErrExist
		}
		if existing.MimeType == FolderMimeType || strings.HasPrefix(existing.MimeType, nativeDocPrefix) {
			return nil, os.ErrInvalid
		}
	case errors.Is(err, os.ErrNotExist):
		if flag&os.O_CREATE == 0 {
			return nil, os.ErrNotExist
		}
		existing = nil
	default:
		return nil, err
	}
	tmp, err := os.CreateTemp("", "sub2api-drive-*")
	if err != nil {
		return nil, err
	}
	return &writeFile{ctx: ctx, c: f.c, tmp: tmp, parentID: parent.ID, name: base, existing: existing, maxBytes: f.maxBytes}, nil
}

// fileInfo implements os.FileInfo plus webdav's optional ContentTyper and
// ETager, so PROPFIND never has to download content to describe a file.
type fileInfo struct {
	name string
	size int64
	mod  time.Time
	dir  bool
	mime string
	etag string
}

func newInfo(file *File, p string) *fileInfo {
	name := path.Base(p)
	info := &fileInfo{name: name, dir: file.MimeType == FolderMimeType, mime: file.MimeType}
	if file.Size != nil {
		info.size = *file.Size
	}
	if t, err := time.Parse(time.RFC3339, file.ModifiedTime); err == nil {
		info.mod = t
	}
	if file.ID != "" {
		info.etag = fmt.Sprintf("%q", file.ID+"-"+file.ModifiedTime)
	}
	return info
}

func (i *fileInfo) Name() string       { return i.name }
func (i *fileInfo) Size() int64        { return i.size }
func (i *fileInfo) ModTime() time.Time { return i.mod }
func (i *fileInfo) IsDir() bool        { return i.dir }
func (i *fileInfo) Sys() any           { return nil }
func (i *fileInfo) Mode() fs.FileMode {
	if i.dir {
		return fs.ModeDir | 0o755
	}
	return 0o644
}

func (i *fileInfo) ContentType(context.Context) (string, error) {
	if i.dir {
		return "", webdav.ErrNotImplemented
	}
	if t := mime.TypeByExtension(path.Ext(i.name)); t != "" {
		return t, nil
	}
	if i.mime != "" && !strings.HasPrefix(i.mime, nativeDocPrefix) {
		return i.mime, nil
	}
	return "application/octet-stream", nil
}

func (i *fileInfo) ETag(context.Context) (string, error) {
	if i.etag == "" {
		return "", webdav.ErrNotImplemented
	}
	return i.etag, nil
}

// dirFile lists a folder's children on demand.
type dirFile struct {
	ctx      context.Context
	c        *Client
	folderID string
	info     *fileInfo
	entries  []os.FileInfo
	loaded   bool
	pos      int
}

func (d *dirFile) load() error {
	if d.loaded {
		return nil
	}
	token := ""
	for pages := 0; ; pages++ {
		if pages >= 1000 {
			return errors.New("Drive folder exceeds page limit")
		}
		page, err := d.c.ListChildren(d.ctx, d.folderID, token, 100)
		if err != nil {
			return err
		}
		for i := range page.Files {
			f := page.Files[i]
			// Names that cannot be expressed as a single path segment are
			// unreachable through WebDAV, so they are not listed.
			if f.Name == "" || f.Name == "." || f.Name == ".." || strings.ContainsAny(f.Name, "/\\\x00") {
				continue
			}
			d.entries = append(d.entries, newInfo(&f, "/"+f.Name))
		}
		if page.NextPageToken == "" {
			break
		}
		token = page.NextPageToken
	}
	d.loaded = true
	return nil
}

func (d *dirFile) Readdir(count int) ([]os.FileInfo, error) {
	if err := d.load(); err != nil {
		return nil, err
	}
	if count <= 0 {
		out := d.entries[d.pos:]
		d.pos = len(d.entries)
		return out, nil
	}
	if d.pos >= len(d.entries) {
		return nil, io.EOF
	}
	end := d.pos + count
	if end > len(d.entries) {
		end = len(d.entries)
	}
	out := d.entries[d.pos:end]
	d.pos = end
	return out, nil
}

func (d *dirFile) Stat() (os.FileInfo, error)     { return d.info, nil }
func (d *dirFile) Read([]byte) (int, error)       { return 0, os.ErrInvalid }
func (d *dirFile) Write([]byte) (int, error)      { return 0, os.ErrInvalid }
func (d *dirFile) Seek(int64, int) (int64, error) { return 0, os.ErrInvalid }
func (d *dirFile) Close() error                   { return nil }

// readFile downloads content into a temp file on first access. Opening a file
// (as PROPFIND does) only uses metadata.
type readFile struct {
	ctx      context.Context
	c        *Client
	file     *File
	info     *fileInfo
	maxBytes int64
	tmp      *os.File
}

func (r *readFile) materialize() error {
	if r.tmp != nil {
		return nil
	}
	if strings.HasPrefix(r.file.MimeType, nativeDocPrefix) {
		return os.ErrInvalid
	}
	if r.maxBytes > 0 && r.info.size > r.maxBytes {
		return ErrTooLarge
	}
	body, err := r.c.Download(r.ctx, r.file.ID)
	if err != nil {
		return err
	}
	defer body.Close()
	tmp, err := os.CreateTemp("", "sub2api-drive-*")
	if err != nil {
		return err
	}
	src := io.Reader(body)
	if r.maxBytes > 0 {
		src = io.LimitReader(body, r.maxBytes+1)
	}
	n, err := io.Copy(tmp, src)
	if err == nil && r.maxBytes > 0 && n > r.maxBytes {
		err = ErrTooLarge
	}
	if err == nil {
		_, err = tmp.Seek(0, io.SeekStart)
	}
	if err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	r.tmp = tmp
	return nil
}

func (r *readFile) Read(p []byte) (int, error) {
	if err := r.materialize(); err != nil {
		return 0, err
	}
	return r.tmp.Read(p)
}

func (r *readFile) Seek(offset int64, whence int) (int64, error) {
	// http.ServeContent seeks to the end to learn the size; answer that from
	// metadata so HEAD requests do not download the file.
	if r.tmp == nil && offset == 0 && (whence == io.SeekEnd || whence == io.SeekStart) {
		if whence == io.SeekEnd {
			return r.info.size, nil
		}
		return 0, nil
	}
	if err := r.materialize(); err != nil {
		return 0, err
	}
	return r.tmp.Seek(offset, whence)
}

func (r *readFile) Readdir(int) ([]os.FileInfo, error) { return nil, os.ErrInvalid }
func (r *readFile) Stat() (os.FileInfo, error)         { return r.info, nil }
func (r *readFile) Write([]byte) (int, error)          { return 0, os.ErrInvalid }

func (r *readFile) Close() error {
	if r.tmp == nil {
		return nil
	}
	name := r.tmp.Name()
	err := r.tmp.Close()
	os.Remove(name)
	return err
}

// writeFile buffers to a temp file and uploads when closed, replacing the
// existing file's content in place so its ID and sharing are preserved.
type writeFile struct {
	ctx      context.Context
	c        *Client
	tmp      *os.File
	parentID string
	name     string
	existing *File
	maxBytes int64
	size     int64
	failed   error
	closed   bool
}

func (w *writeFile) Write(p []byte) (int, error) {
	if w.failed != nil {
		return 0, w.failed
	}
	if w.maxBytes > 0 && w.size+int64(len(p)) > w.maxBytes {
		w.failed = ErrTooLarge
		return 0, w.failed
	}
	n, err := w.tmp.Write(p)
	w.size += int64(n)
	if err != nil {
		w.failed = err
	}
	return n, err
}

func (w *writeFile) Close() error {
	if w.closed {
		return nil
	}
	w.closed = true
	name := w.tmp.Name()
	defer os.Remove(name)
	defer w.tmp.Close()
	if w.failed != nil {
		return w.failed
	}
	if _, err := w.tmp.Seek(0, io.SeekStart); err != nil {
		return err
	}
	ctype := mime.TypeByExtension(path.Ext(w.name))
	if ctype == "" {
		ctype = "application/octet-stream"
	}
	var err error
	if w.existing != nil {
		_, err = w.c.ReplaceContent(w.ctx, w.existing.ID, ctype, w.tmp)
	} else {
		_, err = w.c.Upload(w.ctx, w.name, w.parentID, ctype, w.tmp)
	}
	return err
}

func (w *writeFile) Stat() (os.FileInfo, error) {
	return &fileInfo{name: w.name, size: w.size, mod: time.Now()}, nil
}

func (w *writeFile) Read([]byte) (int, error)           { return 0, os.ErrInvalid }
func (w *writeFile) Seek(int64, int) (int64, error)     { return 0, os.ErrInvalid }
func (w *writeFile) Readdir(int) ([]os.FileInfo, error) { return nil, os.ErrInvalid }
