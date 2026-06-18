package catalog

import (
	"github.com/gin-gonic/gin"
	"github.com/worryyy/devops-platform/platform/server/internal/pkg/bizerr"
	"github.com/worryyy/devops-platform/platform/server/internal/pkg/responses"
)

type serviceURI struct {
	ServiceName string `uri:"serviceName" binding:"required"`
}

func bindURI(c *gin.Context, req any) bool {
	if err := c.ShouldBindUri(req); err != nil {
		responses.Fail(c, bizerr.Param("invalid request parameters"))
		return false
	}
	return true
}
