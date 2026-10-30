package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"os"
	"path"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/googledrive"

	"golang.org/x/net/webdav"
)

// driveFS exposes one user's virtual tree as a webdav.FileSystem. Every path
// is resolved inside that user's tree, so other users' files are unreachable.
type driveFS struct {
	s      *GoogleDriveService
	userID int64
}

func (f *driveFS) Mkdir(ctx context.Context, name string, _ os.FileMode) error {
	return f.s.Mkdir(ctx, f.userID, name)
}

func (f *driveFS) Stat(ctx context.Context, name string) (os.FileInfo, error) {
	node, err := f.s.resolve(ctx, f.userID, name)
	if err != nil {
		return nil, err
	}
	return nodeInfo(node), nil
}

func (f *driveFS) RemoveAll(ctx context.Context, name string) error {
	err := f.s.Delete(ctx, f.userID, name)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (f *driveFS) Rename(ctx context.Context, oldName, newName string) error {
	if _, err := f.s.resolve(ctx, f.userID, newName); err == nil {
		return os.ErrExist
	}
	return f.s.Move(ctx, f.userID, oldName, newName)
}

func (f *driveFS) OpenFile(ctx context.Context, name string, flag int, _ os.FileMode) (webdav.File, error) {
	if flag&(os.O_WRONLY|os.O_RDWR|os.O_CREATE|os.O_TRUNC|os.O_APPEND) != 0 {
		return f.openWrite(ctx, name, flag)
	}
	node, err := f.s.resolve(ctx, f.userID, name)
	if err != nil {
		return nil, err
	}
	if node.IsDir {
		return &driveDir{ctx: ctx, s: f.s, node: node, info: nodeInfo(node)}, nil
	}
	return f.s.openRead(ctx, node), nil
}

func (f *driveFS) openWrite(ctx context.Context, name string, flag int) (webdav.File, error) {
	if flag&os.O_APPEND != 0 {
		return nil, os.ErrInvalid
	}
	parent, base, err := f.s.parentOf(ctx, f.userID, name)
	if err != nil {
		return nil, err
	}
	existing, err := f.s.store.Child(ctx, f.userID, parent.ID, base)
	switch {
	case err == nil:
		if flag&os.O_EXCL != 0 {
			return nil, os.ErrExist
		}
		if existing.IsDir {
			return nil, os.ErrInvalid
		}
	case errors.Is(err, os.ErrNotExist):
		if flag&os.O_CREATE == 0 {
			return nil, os.ErrNotExist
		}
	default:
		return nil, err
	}
	tmp, err := os.CreateTemp("", "sub2api-drive-*")
	if err != nil {
		return nil, err
	}
	return &driveWriteFile{ctx: ctx, fs: f, tmp: tmp, path: cleanDrivePath(name), name: base}, nil
}

// driveInfo implements os.FileInfo plus webdav's ContentTyper and ETager, so
// PROPFIND never needs to read content.
type driveInfo struct {
	name string
	size int64
	mod  time.Time
	dir  bool
	mime string
	etag string
}

func nodeInfo(n *DriveNode) *driveInfo {
	info := &driveInfo{name: n.Name, size: n.Size, mod: n.UpdatedAt, dir: n.IsDir, mime: n.MimeType}
	if n.ID > 0 {
		info.etag = fmt.Sprintf("\"%d-%d\"", n.ID, n.UpdatedAt.UnixNano())
	}
	return info
}

func (i *driveInfo) Name() string       { return i.name }
func (i *driveInfo) Size() int64        { return i.size }
func (i *driveInfo) ModTime() time.Time { return i.mod }
func (i *driveInfo) IsDir() bool        { return i.dir }
func (i *driveInfo) Sys() any           { return nil }
func (i *driveInfo) Mode() fs.FileMode {
	if i.dir {
		return fs.ModeDir | 0o755
	}
	return 0o644
}

func (i *driveInfo) ContentType(context.Context) (string, error) {
	if i.dir {
		return "", webdav.ErrNotImplemented
	}
	if t := mime.TypeByExtension(path.Ext(i.name)); t != "" {
		return t, nil
	}
	if i.mime != "" {
		return i.mime, nil
	}
	return "application/octet-stream", nil
}

func (i *driveInfo) ETag(context.Context) (string, error) {
	if i.etag == "" {
		return "", webdav.ErrNotImplemented
	}
	return i.etag, nil
}

// driveDir lists a folder of the tree on demand.
type driveDir struct {
	ctx     context.Context
	s       *GoogleDriveService
	node    *DriveNode
	info    *driveInfo
	entries []os.FileInfo
	loaded  bool
	pos     int
}

func (d *driveDir) Readdir(count int) ([]os.FileInfo, error) {
	if !d.loaded {
		children, err := d.s.store.Children(d.ctx, d.node.UserID, d.node.ID)
		if err != nil {
			return nil, err
		}
		for _, c := range children {
			d.entries = append(d.entries, nodeInfo(c))
		}
		d.loaded = true
	}
	if count <= 0 {
		out := d.entries[d.pos:]
		d.pos = len(d.entries)
		return out, nil
	}
	if d.pos >= len(d.entries) {
		return nil, io.EOF
	}
	end := min(d.pos+count, len(d.entries))
	out := d.entries[d.pos:end]
	d.pos = end
	return out, nil
}

func (d *driveDir) Stat() (os.FileInfo, error)     { return d.info, nil }
func (d *driveDir) Read([]byte) (int, error)       { return 0, os.ErrInvalid }
func (d *driveDir) Write([]byte) (int, error)      { return 0, os.ErrInvalid }
func (d *driveDir) Seek(int64, int) (int64, error) { return 0, os.ErrInvalid }
func (d *driveDir) Close() error                   { return nil }

// driveReadFile streams a stored object. Seeking only moves the offset; the
// next read opens a ranged download, so no content is buffered on disk and
// metadata-only requests never contact Google.
type driveReadFile struct {
	ctx    context.Context
	s      *GoogleDriveService
	node   *DriveNode
	info   *driveInfo
	offset int64
	body   io.ReadCloser
}

func (s *GoogleDriveService) openRead(ctx context.Context, node *DriveNode) *driveReadFile {
	return &driveReadFile{ctx: ctx, s: s, node: node, info: nodeInfo(node)}
}

func (r *driveReadFile) Read(p []byte) (int, error) {
	if r.offset >= r.node.Size {
		return 0, io.EOF
	}
	if r.body == nil {
		c, _, err := r.s.client(r.ctx, r.node.StorageAccountID)
		if err != nil {
			return 0, err
		}
		body, err := c.DownloadRange(r.ctx, r.node.GoogleFileID, r.offset)
		if err != nil {
			return 0, err
		}
		r.body = body
	}
	n, err := r.body.Read(p)
	r.offset += int64(n)
	return n, err
}

func (r *driveReadFile) Seek(offset int64, whence int) (int64, error) {
	var abs int64
	switch whence {
	case io.SeekStart:
		abs = offset
	case io.SeekCurrent:
		abs = r.offset + offset
	case io.SeekEnd:
		abs = r.node.Size + offset
	default:
		return 0, os.ErrInvalid
	}
	if abs < 0 {
		return 0, os.ErrInvalid
	}
	if abs != r.offset && r.body != nil {
		_ = r.body.Close()
		r.body = nil
	}
	r.offset = abs
	return abs, nil
}

func (r *driveReadFile) Close() error {
	if r.body != nil {
		return r.body.Close()
	}
	return nil
}

func (r *driveReadFile) Readdir(int) ([]os.FileInfo, error) { return nil, os.ErrInvalid }
func (r *driveReadFile) Stat() (os.FileInfo, error)         { return r.info, nil }
func (r *driveReadFile) Write([]byte) (int, error)          { return 0, os.ErrInvalid }

// driveWriteFile buffers WebDAV uploads to a temp file, because the target
// account and size must be known before the object is created.
type driveWriteFile struct {
	ctx    context.Context
	fs     *driveFS
	tmp    *os.File
	path   string
	name   string
	size   int64
	failed error
	closed bool
}

func (w *driveWriteFile) Write(p []byte) (int, error) {
	if w.failed != nil {
		return 0, w.failed
	}
	if w.size+int64(len(p)) > w.fs.s.maxBytes {
		w.failed = googledrive.ErrTooLarge
		return 0, w.failed
	}
	n, err := w.tmp.Write(p)
	w.size += int64(n)
	if err != nil {
		w.failed = err
	}
	return n, err
}

func (w *driveWriteFile) Close() error {
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
	return w.fs.s.Upload(w.ctx, w.fs.userID, w.path, ctype, w.size, w.tmp)
}

func (w *driveWriteFile) Stat() (os.FileInfo, error) {
	if w.closed {
		if node, err := w.fs.s.resolve(w.ctx, w.fs.userID, w.path); err == nil {
			return nodeInfo(node), nil
		}
	}
	return &driveInfo{name: w.name, size: w.size, mod: time.Now()}, nil
}

func (w *driveWriteFile) Read([]byte) (int, error)           { return 0, os.ErrInvalid }
func (w *driveWriteFile) Seek(int64, int) (int64, error)     { return 0, os.ErrInvalid }
func (w *driveWriteFile) Readdir(int) ([]os.FileInfo, error) { return nil, os.ErrInvalid }
