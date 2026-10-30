package admin

import (
	"errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"net/http"
)

func resourceGroupError(c *gin.Context, err error) {
	status, msg := 500, "resource group request failed"
	switch {
	case errors.Is(err, service.ErrResourceGroupInput):
		status, msg = 400, err.Error()
	case errors.Is(err, service.ErrResourceGroupMissing):
		status, msg = 404, err.Error()
	case errors.Is(err, service.ErrResourceGroupConflict):
		status, msg = 409, err.Error()
	}
	response.Error(c, status, msg)
}
func (h *GoogleDriveHandler) ResourceGroups(c *gin.Context) {
	data, err := h.svc.ResourceGroups().List(c.Request.Context(), c.Param("kind"))
	if err != nil {
		resourceGroupError(c, err)
		return
	}
	response.Success(c, data)
}
func (h *GoogleDriveHandler) SaveResourceGroup(c *gin.Context) {
	var id int64
	if c.Param("group") != "" {
		var ok bool
		id, ok = idParam(c, "group")
		if !ok {
			return
		}
	}
	var in service.ResourceGroupInput
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 65536)
	if err := c.ShouldBindJSON(&in); err != nil {
		response.BadRequest(c, "invalid resource group payload")
		return
	}
	data, err := h.svc.ResourceGroups().Save(c.Request.Context(), c.Param("kind"), id, in)
	if err != nil {
		resourceGroupError(c, err)
		return
	}
	response.Success(c, data)
}
