package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/Wei-Shaw/sub2api/internal/pkg/colab"
	"github.com/google/uuid"
	"golang.org/x/net/webdav"
	"net/url"
	"os"
	"path"
	"strings"
)

// ComputeWorkspace identifies an immutable, user-owned ZIP object and an argv.
// No platform API key or account-wide Google token is sent to the runtime.
type ComputeWorkspace struct {
	InputPath   string   `json:"input_path"`
	InputNodeID int64    `json:"input_node_id,omitempty"`
	Command     []string `json:"command"`
	Outputs     []string `json:"outputs"`
}

func nullableWorkspace(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return string(b)
}
func validWorkspacePath(p string) bool {
	return p != "" && p != "." && !strings.Contains(p, "\\") && !strings.ContainsRune(p, 0) && !strings.HasPrefix(p, "/") && path.Clean(p) == p && p != ".." && !strings.HasPrefix(p, "../")
}
func (s *ColabService) validateWorkspace(ctx context.Context, user int64, w *ComputeWorkspace) error {
	if len(w.Command) == 0 || len(w.Command) > 128 || len(w.Outputs) > 32 {
		return os.ErrInvalid
	}
	for _, a := range w.Command {
		if len(a) > 4096 || strings.ContainsRune(a, 0) {
			return os.ErrInvalid
		}
	}
	for _, p := range w.Outputs {
		if !validWorkspacePath(p) {
			return os.ErrInvalid
		}
	}
	n, e := s.drive.resolve(ctx, user, w.InputPath)
	if e != nil {
		return e
	}
	if n.IsDir || n.State != "ready" || n.Size > 64<<20 {
		return os.ErrInvalid
	}
	w.InputNodeID = n.ID
	u, e := url.Parse(s.redirect)
	if e != nil || u.Scheme != "https" || u.Host == "" {
		return ErrComputeUnavailable
	}
	return nil
}

//go:embed compute_workspace.py
var workspaceBootstrap string

func (s *ColabService) workspaceCode(ctx context.Context, j *ComputeJob) (string, error) {
	var w ComputeWorkspace
	if e := json.Unmarshal(j.Workspace, &w); e != nil {
		return "", e
	}
	token := make([]byte, 32)
	if _, e := rand.Read(token); e != nil {
		return "", e
	}
	secret := base64.RawURLEncoding.EncodeToString(token)
	sum := sha256.Sum256([]byte(secret))
	if _, e := s.store.db.ExecContext(ctx, `UPDATE colab_jobs SET input_token_hash=$2 WHERE id=$1`, j.ID, hex.EncodeToString(sum[:])); e != nil {
		return "", e
	}
	u, e := url.Parse(s.redirect)
	if e != nil {
		return "", e
	}
	config := map[string]any{"url": u.Scheme + "://" + u.Host + "/compute/v1/transfers/" + j.ID + "/input", "token": secret, "command": w.Command, "outputs": w.Outputs}
	raw, _ := json.Marshal(config)
	return "import json, base64\n_cp_config=json.loads(base64.b64decode(" + fmt.Sprintf("%q", base64.StdEncoding.EncodeToString(raw)) + "))\n" + workspaceBootstrap, nil
}
func (s *ColabService) WorkspaceInput(ctx context.Context, id, token string) (webdav.File, os.FileInfo, error) {
	if len(token) != 43 {
		return nil, nil, os.ErrNotExist
	}
	var owner int64
	var raw []byte
	var expected string
	e := s.store.db.QueryRowContext(ctx, `SELECT user_id,workspace,input_token_hash FROM colab_jobs WHERE id=$1 AND status='running' AND NOT cancel_requested AND NOT runtime_released`, id).Scan(&owner, &raw, &expected)
	sum := sha256.Sum256([]byte(token))
	if e != nil || expected == "" || subtle.ConstantTimeCompare([]byte(expected), []byte(hex.EncodeToString(sum[:]))) != 1 {
		return nil, nil, os.ErrNotExist
	}
	var w ComputeWorkspace
	if json.Unmarshal(raw, &w) != nil {
		return nil, nil, os.ErrNotExist
	}
	node, e := s.store.Node(ctx, owner, w.InputNodeID)
	if e != nil || node.State != "ready" || node.IsDir || node.Size > 64<<20 {
		return nil, nil, os.ErrNotExist
	}
	f := s.drive.openRead(ctx, node)
	return f, f.info, nil
}
func (s *ColabService) saveWorkspaceOutput(ctx context.Context, c *colab.Client, j *ComputeJob, dir string) error {
	sizeResult, e := c.Execute(ctx, j.Runtime, uuid.NewString(), "import os\nprint(os.path.getsize('/tmp/claudepool-artifacts.zip'))")
	if e != nil {
		return e
	}
	var size int64
	if _, e = fmt.Sscan(strings.TrimSpace(sizeResult.Stdout), &size); e != nil || size < 0 || size > 512<<20 {
		return fmt.Errorf("Workspace output archive missing or exceeds 512 MiB")
	}
	ticket, e := s.drive.StartDirectUpload(ctx, j.UserID, dir, "artifacts.zip", "application/zip", size, "")
	if e != nil {
		return e
	}
	success := false
	defer func() {
		if !success {
			_ = s.drive.CancelDirectUpload(context.WithoutCancel(ctx), j.UserID, ticket.UploadID)
		}
	}()
	uri, _ := json.Marshal(ticket.UploadURL)
	code := "import urllib.request\nwith open('/tmp/claudepool-artifacts.zip','rb') as f:\n r=urllib.request.urlopen(urllib.request.Request(" + string(uri) + ",data=f,method='PUT',headers={'Content-Type':'application/zip','Content-Length':str(" + fmt.Sprint(size) + ")}),timeout=240)\n assert r.status==200 or r.status==201\n r.close()\nprint('ARTIFACT_UPLOAD_OK')"
	result, e := c.Execute(ctx, j.Runtime, uuid.NewString(), code)
	if e != nil || result == nil || !strings.Contains(result.Stdout, "ARTIFACT_UPLOAD_OK") {
		return fmt.Errorf("Workspace output upload failed")
	}
	_, e = s.drive.CompleteDirectUpload(ctx, j.UserID, ticket.UploadID)
	success = e == nil
	return e
}

//go:embed compute_cli.py
var ComputeCLI []byte
