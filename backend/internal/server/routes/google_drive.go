package routes

import (
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"

	"github.com/gin-gonic/gin"
)

// registerDriveFileRoutes mounts the file operations of one user's cloud
// drive. owner decides whose tree a request touches.
func registerDriveFileRoutes(g gin.IRoutes, d *admin.GoogleDriveHandler, owner func(*gin.Context) (int64, bool)) {
	g.GET("/files", d.Files(owner))
	g.POST("/files", d.Upload(owner))
	g.DELETE("/files", d.Delete(owner))
	g.GET("/download", d.Download(owner))
	g.POST("/folders", d.Mkdir(owner))
	g.POST("/move", d.Move(owner))
	g.POST("/upload-sessions", d.UploadSession(owner))
	g.POST("/uploads/:upload/complete", d.CompleteUpload(owner))
	g.DELETE("/uploads/:upload", d.CancelUpload(owner))
}

// registerGoogleDriveRoutes mounts storage-pool operations for administrators
// under /api/v1/admin/drive, including access to any user's drive.
func registerGoogleDriveRoutes(adminGroup *gin.RouterGroup, h *handler.Handlers) {
	d := h.Admin.GoogleDrive
	if d == nil {
		return
	}
	g := adminGroup.Group("/drive")
	g.GET("", d.Status)
	g.POST("/accounts/authorize", d.AuthorizeAccount)
	g.POST("/accounts/:account/refresh", d.RefreshAccount)
	g.PUT("/accounts/:account", d.UpdateAccount)
	g.DELETE("/accounts/:account", d.RemoveAccount)
	u := g.Group("/users/:id")
	u.GET("", d.Overview(admin.AdminTarget))
	u.PUT("/quota", d.SetUserQuota)
	registerDriveFileRoutes(u, d, admin.AdminTarget)
}

// registerUserDriveRoutes mounts the signed-in user's own drive under
// /api/v1/user/drive.
func registerUserDriveRoutes(user *gin.RouterGroup, h *handler.Handlers) {
	if h.Admin == nil || h.Admin.GoogleDrive == nil {
		return
	}
	d := h.Admin.GoogleDrive
	g := user.Group("/drive")
	g.GET("", d.Overview(admin.SignedInUser))
	registerDriveFileRoutes(g, d, admin.SignedInUser)
}

// webDAVMethods are the methods served by golang.org/x/net/webdav.
var webDAVMethods = []string{"GET", "HEAD", "PUT", "DELETE", "OPTIONS", "PROPFIND", "PROPPATCH", "MKCOL", "COPY", "MOVE", "LOCK", "UNLOCK"}

// RegisterGoogleDrivePublicRoutes registers the OAuth callback, the API-key
// drive API for scripts (/drive/v1) and WebDAV. WebDAV uses the user's email
// as the Basic username and one of the user's API keys as the password; the
// existing API-key middleware enforces key status, IP restrictions and user status.
func RegisterGoogleDrivePublicRoutes(r *gin.Engine, h *handler.Handlers, apiKeyAuth middleware.APIKeyAuthMiddleware) {
	if h.Admin == nil || h.Admin.GoogleDrive == nil {
		return
	}
	d := h.Admin.GoogleDrive
	r.GET("/auth/drive/callback", d.Callback)
	api := r.Group("/drive/v1", gin.HandlerFunc(apiKeyAuth), middleware.DriveAPIKeyUsable())
	api.GET("", d.Overview(admin.APIKeyOwner))
	registerDriveFileRoutes(api, d, admin.APIKeyOwner)
	dav := r.Group("/webdav", middleware.WebDAVBasicCredentials(), gin.HandlerFunc(apiKeyAuth), middleware.WebDAVMatchUser())
	for _, m := range webDAVMethods {
		dav.Handle(m, "", d.WebDAV)
		dav.Handle(m, "/*path", d.WebDAV)
	}
}
