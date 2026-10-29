package routes

import (
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"

	"github.com/gin-gonic/gin"
)

func registerGoogleDriveRoutes(admin *gin.RouterGroup, h *handler.Handlers) {
	if h.Admin.GoogleDrive == nil {
		return
	}
	g := admin.Group("/google-drive")
	g.GET("", h.Admin.GoogleDrive.Status)
	g.POST("/users/:id/authorize", h.Admin.GoogleDrive.Authorize)
	g.DELETE("/users/:id", h.Admin.GoogleDrive.Disconnect)
	g.GET("/users/:id/overview", h.Admin.GoogleDrive.Overview)
	g.GET("/users/:id/files", h.Admin.GoogleDrive.Files)
	g.POST("/users/:id/files", h.Admin.GoogleDrive.Upload)
	g.POST("/users/:id/upload-sessions", h.Admin.GoogleDrive.UploadSession)
	g.DELETE("/users/:id/files", h.Admin.GoogleDrive.Delete)
	g.GET("/users/:id/download", h.Admin.GoogleDrive.Download)
	g.POST("/users/:id/folders", h.Admin.GoogleDrive.Mkdir)
}

// webDAVMethods are the methods served by golang.org/x/net/webdav.
var webDAVMethods = []string{"GET", "HEAD", "PUT", "DELETE", "OPTIONS", "PROPFIND", "PROPPATCH", "MKCOL", "COPY", "MOVE", "LOCK", "UNLOCK"}

// RegisterGoogleDrivePublicRoutes registers the OAuth callback and WebDAV.
// WebDAV uses the user's email as the Basic username and the user's API key
// as the password; the existing API-key middleware enforces key status,
// IP restrictions and user status.
func RegisterGoogleDrivePublicRoutes(r *gin.Engine, h *handler.Handlers, apiKeyAuth middleware.APIKeyAuthMiddleware) {
	if h.Admin == nil || h.Admin.GoogleDrive == nil {
		return
	}
	r.GET("/auth/drive/callback", h.Admin.GoogleDrive.Callback)
	// Clients ask for a Google upload URL here and send the file bytes to
	// Google directly, so uploads don't consume this server's bandwidth.
	r.POST("/drive/v1/upload-sessions", gin.HandlerFunc(apiKeyAuth), middleware.DriveAPIKeyUsable(), h.Admin.GoogleDrive.UserUploadSession)
	dav := r.Group("/webdav", middleware.WebDAVBasicCredentials(), gin.HandlerFunc(apiKeyAuth), middleware.WebDAVMatchUser())
	for _, m := range webDAVMethods {
		dav.Handle(m, "", h.Admin.GoogleDrive.WebDAV)
		dav.Handle(m, "/*path", h.Admin.GoogleDrive.WebDAV)
	}
}
