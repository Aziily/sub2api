package admin

import (
	"errors"
	"html"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/googledrive"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
	"golang.org/x/net/webdav"
)

// GoogleDriveHandler serves the cloud drive: storage-pool operations for
// administrators, each user's own drive, the OAuth callback and WebDAV.
type GoogleDriveHandler struct {
	svc *service.GoogleDriveService
}

func NewGoogleDriveHandler(svc *service.GoogleDriveService) *GoogleDriveHandler {
	return &GoogleDriveHandler{svc: svc}
}

// driveError maps errors to a status and a message safe to show users.
func driveError(err error) (int, string) {
	var apiErr *googledrive.APIError
	var maxErr *http.MaxBytesError
	switch {
	case errors.Is(err, service.ErrDriveDisabled), errors.Is(err, service.ErrDriveNoStorage):
		return http.StatusServiceUnavailable, err.Error()
	case errors.Is(err, service.ErrDriveQuotaExceeded):
		return http.StatusInsufficientStorage, err.Error()
	case errors.Is(err, service.ErrDriveUploadNotFound):
		return http.StatusConflict, err.Error()
	case errors.Is(err, service.ErrDriveAccountInUse), errors.Is(err, service.ErrDriveAccountMismatch):
		return http.StatusConflict, err.Error()
	case errors.Is(err, service.ErrUserNotFound):
		return http.StatusNotFound, "user not found"
	case errors.Is(err, os.ErrNotExist):
		return http.StatusNotFound, "path not found"
	case errors.Is(err, os.ErrExist):
		return http.StatusConflict, "an item with this name already exists"
	case errors.Is(err, googledrive.ErrTooLarge), errors.As(err, &maxErr):
		return http.StatusRequestEntityTooLarge, googledrive.ErrTooLarge.Error()
	case errors.Is(err, service.ErrDriveIsFolder), errors.Is(err, os.ErrInvalid), errors.Is(err, os.ErrPermission):
		return http.StatusBadRequest, "operation is not allowed for this path"
	case errors.As(err, &apiErr):
		return http.StatusBadGateway, apiErr.Error()
	default:
		return http.StatusInternalServerError, "cloud drive request failed"
	}
}

func (h *GoogleDriveHandler) fail(c *gin.Context, err error) {
	status, msg := driveError(err)
	response.Error(c, status, msg)
}

func idParam(c *gin.Context, name string) (int64, bool) {
	id, err := strconv.ParseInt(c.Param(name), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "invalid "+name)
		return 0, false
	}
	return id, true
}

// ownerFunc identifies whose tree a request operates on.
type ownerFunc func(*gin.Context) (int64, bool)

// AdminTarget: /api/v1/admin/drive/users/:id/... (administrator acting on a user)
func AdminTarget(c *gin.Context) (int64, bool) { return idParam(c, "id") }

// SignedInUser: /api/v1/user/drive/... (the signed-in user's own drive)
func SignedInUser(c *gin.Context) (int64, bool) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		response.Unauthorized(c, "sign in required")
		return 0, false
	}
	return subject.UserID, true
}

// APIKeyOwner: /drive/v1/... (the user that owns the API key)
func APIKeyOwner(c *gin.Context) (int64, bool) {
	key, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok || key == nil || key.User == nil {
		c.AbortWithStatus(http.StatusUnauthorized)
		return 0, false
	}
	return key.User.ID, true
}

// ---- administrator: configuration and storage pool ----

// Status GET /api/v1/admin/drive
func (h *GoogleDriveHandler) Status(c *gin.Context) {
	st, err := h.svc.AdminStatus(c.Request.Context(), c.Request.Host)
	if err != nil {
		response.InternalError(c, "failed to read cloud drive status")
		return
	}
	response.Success(c, st)
}

type driveAuthorizeRequest struct {
	AccountID int64 `json:"account_id"`
}

// AuthorizeAccount POST /api/v1/admin/drive/accounts/authorize
// adds a storage account (account_id 0) or renews an existing one.
func (h *GoogleDriveHandler) AuthorizeAccount(c *gin.Context) {
	var req driveAuthorizeRequest
	if c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&req); err != nil || req.AccountID < 0 {
			response.BadRequest(c, "invalid account_id")
			return
		}
	}
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		// Admin API keys have no user identity to bind the authorization to.
		response.Forbidden(c, "sign in as an administrator to connect storage")
		return
	}
	authURL, err := h.svc.StartAccountAuthorization(c.Request.Context(), subject.UserID, req.AccountID)
	if err != nil {
		h.fail(c, err)
		return
	}
	response.Success(c, gin.H{"auth_url": authURL})
}

// RefreshAccount POST /api/v1/admin/drive/accounts/:account/refresh
func (h *GoogleDriveHandler) RefreshAccount(c *gin.Context) {
	id, ok := idParam(c, "account")
	if !ok {
		return
	}
	account, err := h.svc.RefreshQuota(c.Request.Context(), id)
	if err != nil {
		h.fail(c, err)
		return
	}
	response.Success(c, account)
}

type driveAccountUpdate struct {
	Enabled *bool `json:"enabled" binding:"required"`
}

// UpdateAccount PUT /api/v1/admin/drive/accounts/:account
func (h *GoogleDriveHandler) UpdateAccount(c *gin.Context) {
	id, ok := idParam(c, "account")
	if !ok {
		return
	}
	var req driveAccountUpdate
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "enabled is required")
		return
	}
	if err := h.svc.SetAccountEnabled(c.Request.Context(), id, *req.Enabled); err != nil {
		h.fail(c, err)
		return
	}
	response.Success(c, gin.H{"enabled": *req.Enabled})
}

// RemoveAccount DELETE /api/v1/admin/drive/accounts/:account
func (h *GoogleDriveHandler) RemoveAccount(c *gin.Context) {
	id, ok := idParam(c, "account")
	if !ok {
		return
	}
	if err := h.svc.RemoveAccount(c.Request.Context(), id); err != nil {
		h.fail(c, err)
		return
	}
	response.Success(c, gin.H{"removed": true})
}

type driveQuotaRequest struct {
	QuotaBytes *int64 `json:"quota_bytes"`
}

// SetUserQuota PUT /api/v1/admin/drive/users/:id/quota ({"quota_bytes": null} removes it)
func (h *GoogleDriveHandler) SetUserQuota(c *gin.Context) {
	userID, ok := AdminTarget(c)
	if !ok {
		return
	}
	var req driveQuotaRequest
	if err := c.ShouldBindJSON(&req); err != nil || (req.QuotaBytes != nil && *req.QuotaBytes < 0) {
		response.BadRequest(c, "quota_bytes must be a non-negative number or null")
		return
	}
	if err := h.svc.SetUserQuota(c.Request.Context(), userID, req.QuotaBytes); err != nil {
		h.fail(c, err)
		return
	}
	response.Success(c, gin.H{"quota_bytes": req.QuotaBytes})
}

// Callback GET /auth/drive/callback (public; authorized by the one-time state)
func (h *GoogleDriveHandler) Callback(c *gin.Context) {
	if e := c.Query("error"); e != "" {
		writeCallbackPage(c, http.StatusBadRequest, "Authorization was not completed: "+e)
		return
	}
	account, err := h.svc.CompleteAuthorization(c.Request.Context(), c.Query("state"), c.Query("code"))
	if err != nil {
		msg := "Authorization failed. Start it again from the cloud drive storage page."
		if errors.Is(err, service.ErrDriveAccountMismatch) || strings.Contains(err.Error(), "refresh token") ||
			strings.Contains(err.Error(), "expired authorization") || strings.Contains(err.Error(), "no longer reachable") {
			msg = err.Error()
		}
		writeCallbackPage(c, http.StatusBadRequest, msg)
		return
	}
	writeCallbackPage(c, http.StatusOK, account.GoogleEmail+" is connected to the storage pool. You can close this page.")
}

func writeCallbackPage(c *gin.Context, status int, message string) {
	c.Header("Cache-Control", "no-store")
	c.Header("Referrer-Policy", "no-referrer")
	c.Data(status, "text/html; charset=utf-8", []byte(`<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>Cloud drive storage</title></head><body style="font-family:sans-serif;max-width:40rem;margin:4rem auto;padding:0 1rem"><h1>Cloud drive storage</h1><p>`+html.EscapeString(message)+`</p></body></html>`))
}

// ---- a user's drive (shared by the admin, user and API-key routes) ----

// Overview returns the owner's usage, remaining space and WebDAV details.
func (h *GoogleDriveHandler) Overview(owner ownerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, ok := owner(c)
		if !ok {
			return
		}
		ov, err := h.svc.Overview(c.Request.Context(), userID)
		if err != nil {
			h.fail(c, err)
			return
		}
		response.Success(c, gin.H{"overview": ov, "webdav_path": "/webdav/"})
	}
}

func (h *GoogleDriveHandler) Files(owner ownerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, ok := owner(c)
		if !ok {
			return
		}
		dir := c.DefaultQuery("path", "/")
		entries, err := h.svc.List(c.Request.Context(), userID, dir)
		if err != nil {
			h.fail(c, err)
			return
		}
		response.Success(c, gin.H{"path": path.Clean("/" + dir), "items": entries})
	}
}

// Upload relays a multipart "file" through this server (fallback when the
// client cannot reach Google). POST .../files?path=/dir
func (h *GoogleDriveHandler) Upload(owner ownerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, ok := owner(c)
		if !ok {
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, h.svc.MaxBytes()+(1<<20))
		reader, err := c.Request.MultipartReader()
		if err != nil {
			response.BadRequest(c, "expected a multipart upload")
			return
		}
		for {
			part, err := reader.NextPart()
			if err == io.EOF {
				response.BadRequest(c, "missing file field")
				return
			}
			if err != nil {
				h.fail(c, err)
				return
			}
			if part.FormName() != "file" {
				continue
			}
			name := path.Base(strings.ReplaceAll(part.FileName(), "\\", "/"))
			err = h.relay(c, userID, path.Join(path.Clean("/"+c.DefaultQuery("path", "/")), name), part.Header.Get("Content-Type"), part)
			_ = part.Close()
			if err != nil {
				h.fail(c, err)
				return
			}
			response.Success(c, gin.H{"name": name})
			return
		}
	}
}

// relay buffers a part of unknown length so its exact size is known.
func (h *GoogleDriveHandler) relay(c *gin.Context, userID int64, target, contentType string, content io.Reader) error {
	tmp, err := os.CreateTemp("", "sub2api-drive-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	n, err := io.Copy(tmp, io.LimitReader(content, h.svc.MaxBytes()+1))
	if err != nil {
		return err
	}
	if n > h.svc.MaxBytes() {
		return googledrive.ErrTooLarge
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return err
	}
	return h.svc.Upload(c.Request.Context(), userID, target, contentType, n, tmp)
}

type driveUploadSessionRequest struct {
	Path     string `json:"path"`
	Name     string `json:"name" binding:"required"`
	Size     *int64 `json:"size" binding:"required"`
	MimeType string `json:"mime_type"`
}

// requestOrigin returns the caller's browser origin, or "" for non-browser
// clients. Google only accepts cross-origin PUTs from the session's origin.
func requestOrigin(c *gin.Context) string {
	o := c.GetHeader("Origin")
	u, err := url.Parse(o)
	if o == "" || err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.Path != "" || u.RawQuery != "" || u.User != nil {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

// UploadSession POST .../upload-sessions reserves space and returns a Google
// upload URL; the client PUTs the bytes there and then calls complete.
func (h *GoogleDriveHandler) UploadSession(owner ownerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, ok := owner(c)
		if !ok {
			return
		}
		var req driveUploadSessionRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			response.BadRequest(c, "name and size are required")
			return
		}
		if req.Path == "" {
			req.Path = "/"
		}
		ticket, err := h.svc.StartDirectUpload(c.Request.Context(), userID, req.Path, req.Name, req.MimeType, *req.Size, requestOrigin(c))
		if err != nil {
			h.fail(c, err)
			return
		}
		// The URL can upload without further authentication; keep it out of caches.
		c.Header("Cache-Control", "no-store")
		response.Success(c, ticket)
	}
}

// CompleteUpload POST .../uploads/:upload/complete
func (h *GoogleDriveHandler) CompleteUpload(owner ownerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, ok := owner(c)
		if !ok {
			return
		}
		id, ok := idParam(c, "upload")
		if !ok {
			return
		}
		entry, err := h.svc.CompleteDirectUpload(c.Request.Context(), userID, id)
		if err != nil {
			h.fail(c, err)
			return
		}
		response.Success(c, entry)
	}
}

// CancelUpload DELETE .../uploads/:upload
func (h *GoogleDriveHandler) CancelUpload(owner ownerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, ok := owner(c)
		if !ok {
			return
		}
		id, ok := idParam(c, "upload")
		if !ok {
			return
		}
		if err := h.svc.CancelDirectUpload(c.Request.Context(), userID, id); err != nil {
			h.fail(c, err)
			return
		}
		response.Success(c, gin.H{"cancelled": true})
	}
}

// Download GET .../download?path=/a.txt
func (h *GoogleDriveHandler) Download(owner ownerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, ok := owner(c)
		if !ok {
			return
		}
		f, fi, err := h.svc.Open(c.Request.Context(), userID, c.Query("path"))
		if err != nil {
			h.fail(c, err)
			return
		}
		defer f.Close()
		c.Header("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(fi.Name()))
		c.Header("Cache-Control", "no-store")
		http.ServeContent(c.Writer, c.Request, fi.Name(), fi.ModTime(), f)
	}
}

type drivePathRequest struct {
	Path string `json:"path" binding:"required"`
}

// Mkdir POST .../folders {"path": "/a/b"}
func (h *GoogleDriveHandler) Mkdir(owner ownerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, ok := owner(c)
		if !ok {
			return
		}
		var req drivePathRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			response.BadRequest(c, "path is required")
			return
		}
		if err := h.svc.Mkdir(c.Request.Context(), userID, req.Path); err != nil {
			h.fail(c, err)
			return
		}
		response.Success(c, gin.H{"path": path.Clean("/" + req.Path)})
	}
}

type driveMoveRequest struct {
	From string `json:"from" binding:"required"`
	To   string `json:"to" binding:"required"`
}

// Move POST .../move {"from": "/a", "to": "/b"}
func (h *GoogleDriveHandler) Move(owner ownerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, ok := owner(c)
		if !ok {
			return
		}
		var req driveMoveRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			response.BadRequest(c, "from and to are required")
			return
		}
		if err := h.svc.Move(c.Request.Context(), userID, req.From, req.To); err != nil {
			h.fail(c, err)
			return
		}
		response.Success(c, gin.H{"path": path.Clean("/" + req.To)})
	}
}

// Delete DELETE .../files?path=/a.txt (permanent)
func (h *GoogleDriveHandler) Delete(owner ownerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, ok := owner(c)
		if !ok {
			return
		}
		if err := h.svc.Delete(c.Request.Context(), userID, c.Query("path")); err != nil {
			h.fail(c, err)
			return
		}
		response.Success(c, gin.H{"deleted": true})
	}
}

// WebDAV serves /webdav/* for the user that owns the authenticated API key.
// Authentication is done by the WebDAV Basic-auth adapter and API-key middleware.
func (h *GoogleDriveHandler) WebDAV(c *gin.Context) {
	userID, ok := APIKeyOwner(c)
	if !ok {
		return
	}
	fsys, locks, err := h.svc.FileSystem(c.Request.Context(), userID)
	if err != nil {
		status, msg := driveError(err)
		c.String(status, msg)
		return
	}
	// x/net/webdav reports every failed write as 405, which clients show as
	// "not allowed". Check size limits first so they see 507 or 413 instead.
	if c.Request.Method == http.MethodPut && c.Request.ContentLength >= 0 {
		var replaced int64
		if fi, err := fsys.Stat(c.Request.Context(), strings.TrimPrefix(c.Request.URL.Path, "/webdav")); err == nil && !fi.IsDir() {
			replaced = fi.Size()
		}
		if err := h.svc.CheckWrite(c.Request.Context(), userID, c.Request.ContentLength, replaced); err != nil {
			status, msg := driveError(err)
			c.String(status, msg)
			return
		}
	}
	(&webdav.Handler{Prefix: "/webdav", FileSystem: fsys, LockSystem: locks}).ServeHTTP(c.Writer, c.Request)
}
