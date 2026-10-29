package admin

import (
	"errors"
	"fmt"
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

// GoogleDriveHandler serves Drive management, the OAuth callback and WebDAV.
type GoogleDriveHandler struct {
	svc *service.GoogleDriveService
}

func NewGoogleDriveHandler(svc *service.GoogleDriveService) *GoogleDriveHandler {
	return &GoogleDriveHandler{svc: svc}
}

// driveError maps errors to a status and a message safe to show users.
func driveError(err error) (int, string) {
	var apiErr *googledrive.APIError
	switch {
	case errors.Is(err, service.ErrDriveDisabled):
		return http.StatusServiceUnavailable, err.Error()
	case errors.Is(err, service.ErrDriveNotConnected):
		return http.StatusNotFound, err.Error()
	case errors.Is(err, service.ErrUserNotFound):
		return http.StatusNotFound, "user not found"
	case errors.Is(err, os.ErrNotExist):
		return http.StatusNotFound, "path not found"
	case errors.Is(err, os.ErrExist):
		return http.StatusConflict, "an item with this name already exists"
	case errors.Is(err, googledrive.ErrAmbiguousPath):
		return http.StatusConflict, err.Error()
	case errors.Is(err, googledrive.ErrTooLarge):
		return http.StatusRequestEntityTooLarge, err.Error()
	case errors.Is(err, service.ErrDriveIsFolder), errors.Is(err, os.ErrInvalid), errors.Is(err, os.ErrPermission):
		return http.StatusBadRequest, "operation is not allowed for this path"
	case errors.As(err, &apiErr):
		return http.StatusBadGateway, apiErr.Error()
	default:
		return http.StatusInternalServerError, "Google Drive request failed"
	}
}

func (h *GoogleDriveHandler) fail(c *gin.Context, err error) {
	status, msg := driveError(err)
	response.Error(c, status, msg)
}

func userIDParam(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "invalid user ID")
		return 0, false
	}
	return id, true
}

// Status GET /api/v1/admin/google-drive
func (h *GoogleDriveHandler) Status(c *gin.Context) {
	enabled, reason := h.svc.Status()
	users, err := h.svc.ListUsers(c.Request.Context())
	if err != nil {
		response.InternalError(c, "failed to list users")
		return
	}
	response.Success(c, gin.H{"enabled": enabled, "reason": reason, "max_bytes": h.svc.MaxBytes(), "users": users})
}

// Authorize POST /api/v1/admin/google-drive/users/:id/authorize
func (h *GoogleDriveHandler) Authorize(c *gin.Context) {
	userID, ok := userIDParam(c)
	if !ok {
		return
	}
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		// Admin API keys have no user identity to bind the authorization to.
		response.Forbidden(c, "sign in as an administrator to connect Google Drive")
		return
	}
	authURL, err := h.svc.StartAuthorization(c.Request.Context(), subject.UserID, userID)
	if err != nil {
		h.fail(c, err)
		return
	}
	response.Success(c, gin.H{"auth_url": authURL})
}

// Callback GET /auth/drive/callback (public; authorized by the one-time state)
func (h *GoogleDriveHandler) Callback(c *gin.Context) {
	if e := c.Query("error"); e != "" {
		writeCallbackPage(c, http.StatusBadRequest, "Google Drive authorization was not completed: "+e)
		return
	}
	userID, err := h.svc.CompleteAuthorization(c.Request.Context(), c.Query("state"), c.Query("code"))
	if err != nil {
		msg := "Google Drive authorization failed. Start the authorization again from the admin page."
		if strings.Contains(err.Error(), "refresh token") || strings.Contains(err.Error(), "expired authorization") {
			msg = err.Error()
		}
		writeCallbackPage(c, http.StatusBadRequest, msg)
		return
	}
	writeCallbackPage(c, http.StatusOK, fmt.Sprintf("Google Drive is connected for user #%d. You can close this page and refresh the admin page.", userID))
}

func writeCallbackPage(c *gin.Context, status int, message string) {
	c.Header("Cache-Control", "no-store")
	c.Header("Referrer-Policy", "no-referrer")
	c.Data(status, "text/html; charset=utf-8", []byte(`<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>Google Drive</title></head><body style="font-family:sans-serif;max-width:40rem;margin:4rem auto;padding:0 1rem"><h1>Google Drive</h1><p>`+html.EscapeString(message)+`</p></body></html>`))
}

// Disconnect DELETE /api/v1/admin/google-drive/users/:id
func (h *GoogleDriveHandler) Disconnect(c *gin.Context) {
	userID, ok := userIDParam(c)
	if !ok {
		return
	}
	if err := h.svc.Disconnect(c.Request.Context(), userID); err != nil {
		h.fail(c, err)
		return
	}
	response.Success(c, gin.H{"disconnected": true})
}

// Overview GET /api/v1/admin/google-drive/users/:id/overview
func (h *GoogleDriveHandler) Overview(c *gin.Context) {
	userID, ok := userIDParam(c)
	if !ok {
		return
	}
	overview, err := h.svc.Overview(c.Request.Context(), userID)
	if err != nil {
		h.fail(c, err)
		return
	}
	response.Success(c, overview)
}

// Files GET /api/v1/admin/google-drive/users/:id/files?path=/
func (h *GoogleDriveHandler) Files(c *gin.Context) {
	userID, ok := userIDParam(c)
	if !ok {
		return
	}
	entries, err := h.svc.List(c.Request.Context(), userID, c.DefaultQuery("path", "/"))
	if err != nil {
		h.fail(c, err)
		return
	}
	response.Success(c, gin.H{"path": path.Clean("/" + c.DefaultQuery("path", "/")), "items": entries})
}

// Upload POST /api/v1/admin/google-drive/users/:id/files?path=/dir (multipart "file")
func (h *GoogleDriveHandler) Upload(c *gin.Context) {
	userID, ok := userIDParam(c)
	if !ok {
		return
	}
	// Allow multipart framing overhead on top of the file size limit.
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
			response.BadRequest(c, "invalid multipart upload")
			return
		}
		if part.FormName() != "file" {
			continue
		}
		name := path.Base(strings.ReplaceAll(part.FileName(), "\\", "/"))
		err = h.svc.Upload(c.Request.Context(), userID, c.DefaultQuery("path", "/"), name, part)
		_ = part.Close()
		if err != nil {
			var maxErr *http.MaxBytesError
			if errors.As(err, &maxErr) {
				err = googledrive.ErrTooLarge
			}
			h.fail(c, err)
			return
		}
		response.Success(c, gin.H{"name": name})
		return
	}
}

// Download GET /api/v1/admin/google-drive/users/:id/download?path=/a.txt
func (h *GoogleDriveHandler) Download(c *gin.Context) {
	userID, ok := userIDParam(c)
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

type driveFolderRequest struct {
	Path string `json:"path" binding:"required"`
}

// Mkdir POST /api/v1/admin/google-drive/users/:id/folders
func (h *GoogleDriveHandler) Mkdir(c *gin.Context) {
	userID, ok := userIDParam(c)
	if !ok {
		return
	}
	var req driveFolderRequest
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

// Delete DELETE /api/v1/admin/google-drive/users/:id/files?path=/a.txt
func (h *GoogleDriveHandler) Delete(c *gin.Context) {
	userID, ok := userIDParam(c)
	if !ok {
		return
	}
	if err := h.svc.Delete(c.Request.Context(), userID, c.Query("path")); err != nil {
		h.fail(c, err)
		return
	}
	response.Success(c, gin.H{"trashed": true})
}

// WebDAV serves /webdav/* for the user that owns the authenticated API key.
// Authentication is done by the WebDAV Basic-auth adapter and API-key middleware.
func (h *GoogleDriveHandler) WebDAV(c *gin.Context) {
	key, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok || key == nil || key.User == nil {
		c.AbortWithStatus(http.StatusUnauthorized)
		return
	}
	fsys, locks, err := h.svc.FileSystem(c.Request.Context(), key.User.ID)
	if err != nil {
		status, msg := driveError(err)
		c.String(status, msg)
		return
	}
	(&webdav.Handler{Prefix: "/webdav", FileSystem: fsys, LockSystem: locks}).ServeHTTP(c.Writer, c.Request)
}
