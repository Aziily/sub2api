package routes

import (
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
)

func registerColabAdminRoutes(g *gin.RouterGroup, h *handler.Handlers) {
	d := h.Admin.Colab
	if d == nil {
		return
	}
	c := g.Group("/compute")
	c.GET("", d.Status(true))
	c.POST("/accounts/authorize", d.Authorize)
	c.POST("/accounts/:account/refresh", d.Refresh)
	c.PUT("/accounts/:account", d.Enable)
	c.GET("/jobs", d.Jobs(admin.SignedInUser, true))
	c.GET("/jobs/:job", d.Job(admin.SignedInUser, true))
	c.POST("/jobs", d.Submit(admin.SignedInUser))
	c.POST("/jobs/:job/cancel", d.Cancel(admin.SignedInUser, true))
}
func registerColabUserRoutes(g *gin.RouterGroup, h *handler.Handlers) {
	if h.Admin == nil || h.Admin.Colab == nil {
		return
	}
	d := h.Admin.Colab
	c := g.Group("/compute")
	c.GET("", d.Status(false))
	c.GET("/jobs", d.Jobs(admin.SignedInUser, false))
	c.GET("/jobs/:job", d.Job(admin.SignedInUser, false))
	c.POST("/jobs", d.Submit(admin.SignedInUser))
	c.POST("/jobs/:job/cancel", d.Cancel(admin.SignedInUser, false))
}
func registerColabPublicRoutes(r *gin.Engine, h *handler.Handlers, auth middleware.APIKeyAuthMiddleware) {
	if h.Admin == nil || h.Admin.Colab == nil {
		return
	}
	d := h.Admin.Colab
	r.GET("/auth/colab/callback", d.Callback)
	g := r.Group("/compute/v1", gin.HandlerFunc(auth), middleware.DriveAPIKeyUsable())
	g.GET("/status", d.Status(false))
	g.GET("/jobs", d.Jobs(admin.APIKeyOwner, false))
	g.GET("/jobs/:job", d.Job(admin.APIKeyOwner, false))
	g.POST("/jobs", d.Submit(admin.APIKeyOwner))
	g.POST("/jobs/:job/cancel", d.Cancel(admin.APIKeyOwner, false))
}
