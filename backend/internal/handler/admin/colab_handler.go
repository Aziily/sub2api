package admin

import (
	"database/sql"
	"errors"
	"io"
	"net/http"
	"os"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type ColabHandler struct{ svc *service.ColabService }

func NewColabHandler(s *service.ColabService) *ColabHandler { return &ColabHandler{s} }
func (h *ColabHandler) fail(c *gin.Context, e error) {
	code, msg := 500, "Compute operation failed"
	switch {
	case errors.Is(e, service.ErrComputeBalance):
		code, msg = 402, e.Error()
	case errors.Is(e, service.ErrComputePrice), errors.Is(e, service.ErrDriveQuotaExceeded):
		code, msg = 422, e.Error()
	case errors.Is(e, sql.ErrNoRows):
		code, msg = 404, "Job or account not found"
	case errors.Is(e, os.ErrInvalid):
		code, msg = 400, "Use a UUID job ID, valid hardware (CPU/T4/L4/G4/A100/A100_HIGHMEM/CPU_HIGHMEM), 1–65536 bytes of Python and a 30–3600 second timeout"
	case errors.Is(e, service.ErrComputeHardwareUnavailable):
		code, msg = 422, e.Error()
	case errors.Is(e, service.ErrComputeUnavailable):
		code, msg = 503, e.Error()
	case errors.Is(e, service.ErrComputeConflict):
		code, msg = 409, e.Error()
	}
	response.Error(c, code, msg)
}
func (h *ColabHandler) Status(owner ownerFunc, admin bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		u, ok := owner(c)
		if !ok {
			return
		}
		v, e := h.svc.Status(c.Request.Context(), u, admin)
		if e != nil {
			h.fail(c, e)
			return
		}
		response.Success(c, v)
	}
}
func (h *ColabHandler) Authorize(c *gin.Context) {
	id, ok := SignedInUser(c)
	if !ok {
		return
	}
	u, e := h.svc.Authorize(id)
	if e != nil {
		h.fail(c, e)
		return
	}
	c.Header("Cache-Control", "no-store")
	response.Success(c, gin.H{"url": u})
}
func (h *ColabHandler) Callback(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("Referrer-Policy", "no-referrer")
	if e := h.svc.Callback(c.Request.Context(), c.Query("state"), c.Query("code")); e != nil {
		c.Data(400, "text/html; charset=utf-8", []byte(`<h1>Colab 授权或权限检查未完成</h1><p>返回后台查看账号状态。授权链接有效期为十分钟；若未保存账号，请重新发起授权。</p><a href="/admin/compute">返回计算管理</a>`))
		return
	}
	c.Redirect(http.StatusSeeOther, "/admin/compute?connected=1")
}
func (h *ColabHandler) Refresh(c *gin.Context) {
	id, ok := idParam(c, "account")
	if !ok {
		return
	}
	if e := h.svc.RefreshAccount(c.Request.Context(), id); e != nil {
		response.Error(c, 502, e.Error())
		return
	}
	response.Success(c, gin.H{"ok": true})
}
func (h *ColabHandler) Enable(c *gin.Context) {
	id, ok := idParam(c, "account")
	if !ok {
		return
	}
	var in struct {
		Enabled bool `json:"enabled"`
	}
	if c.ShouldBindJSON(&in) != nil {
		response.BadRequest(c, "Invalid JSON")
		return
	}
	if e := h.svc.SetEnabled(c.Request.Context(), id, in.Enabled); e != nil {
		h.fail(c, e)
		return
	}
	response.Success(c, gin.H{"ok": true})
}
func (h *ColabHandler) Jobs(owner ownerFunc, admin bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		u, ok := owner(c)
		if !ok {
			return
		}
		v, e := h.svc.Jobs(c.Request.Context(), u, admin)
		if e != nil {
			h.fail(c, e)
			return
		}
		response.Success(c, v)
	}
}
func (h *ColabHandler) Job(owner ownerFunc, admin bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		u, ok := owner(c)
		if !ok {
			return
		}
		v, e := h.svc.Job(c.Request.Context(), u, c.Param("job"), admin)
		if e != nil {
			h.fail(c, e)
			return
		}
		response.Success(c, v)
	}
}
func (h *ColabHandler) Submit(owner ownerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		u, ok := owner(c)
		if !ok {
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 70000)
		var in struct {
			ID        string                    `json:"id"`
			Title     string                    `json:"title"`
			Code      string                    `json:"code"`
			Hardware  string                    `json:"hardware"`
			Timeout   int                       `json:"timeout_seconds"`
			Workspace *service.ComputeWorkspace `json:"workspace"`
		}
		if c.ShouldBindJSON(&in) != nil {
			response.BadRequest(c, "Invalid or oversized job")
			return
		}
		if in.Timeout == 0 {
			in.Timeout = 180
		}
		if in.Hardware == "" {
			in.Hardware = "CPU"
		}
		v, e := h.svc.Submit(c.Request.Context(), u, in.ID, in.Title, in.Code, in.Hardware, in.Timeout, in.Workspace)
		if e != nil {
			h.fail(c, e)
			return
		}
		response.Success(c, v)
	}
}
func (h *ColabHandler) Cancel(owner ownerFunc, admin bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		u, ok := owner(c)
		if !ok {
			return
		}
		if e := h.svc.Cancel(c.Request.Context(), u, c.Param("job"), admin); e != nil {
			h.fail(c, e)
			return
		}
		response.Success(c, gin.H{"ok": true})
	}
}

// A transfer credential can read only the immutable input object of one active job.
func (h *ColabHandler) Input(c *gin.Context) {
	f, info, e := h.svc.WorkspaceInput(c.Request.Context(), c.Param("job"), c.GetHeader("X-Compute-Transfer"))
	if e != nil {
		response.Error(c, 404, "Transfer not available")
		return
	}
	defer f.Close()
	c.Header("Cache-Control", "no-store")
	c.Header("Content-Type", "application/zip")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Status(200)
	_, _ = io.CopyN(c.Writer, f, info.Size())
}

func (h *ColabHandler) SetPricing(c *gin.Context) {
	var p service.ComputePricing
	if c.ShouldBindJSON(&p) != nil {
		response.BadRequest(c, "Invalid prices")
		return
	}
	if e := h.svc.SetPricing(c.Request.Context(), p); e != nil {
		h.fail(c, e)
		return
	}
	response.Success(c, gin.H{"ok": true})
}
