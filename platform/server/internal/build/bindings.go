package build

import (
	"github.com/gin-gonic/gin"
	"github.com/worryyy/devops-platform/platform/server/internal/pkg/bizerr"
	"github.com/worryyy/devops-platform/platform/server/internal/pkg/responses"
)

type buildURI struct {
	BuildID string `uri:"buildId" binding:"required"`
}

type buildListQuery struct {
	ServiceName string `form:"service"`
	Branch      string `form:"branch"`
	Status      Status `form:"status"`
	Builder     string `form:"builder"`
	Limit       int    `form:"limit"`
	Offset      int    `form:"offset"`
}

type recentImagesURI struct {
	ServiceName string `uri:"serviceName" binding:"required"`
}

type recentImagesQuery struct {
	Limit int `form:"limit"`
}

type rebuildRequest struct {
	Builder string `json:"builder"`
}

func bindJSON(c *gin.Context, req any) bool {
	if err := c.ShouldBindJSON(req); err != nil {
		responses.Fail(c, bizerr.Param("invalid request body"))
		return false
	}
	return true
}

func bindOptionalJSON(c *gin.Context, req any) {
	_ = c.ShouldBindJSON(req)
}

func bindQuery(c *gin.Context, req any) bool {
	if err := c.ShouldBindQuery(req); err != nil {
		responses.Fail(c, bizerr.Param("invalid request parameters"))
		return false
	}
	return true
}

func bindURI(c *gin.Context, req any) bool {
	if err := c.ShouldBindUri(req); err != nil {
		responses.Fail(c, bizerr.Param("invalid request parameters"))
		return false
	}
	return true
}

func (q buildListQuery) filter() ListFilter {
	limit := q.Limit
	if limit <= 0 {
		limit = 50
	}
	return ListFilter{
		ServiceName: q.ServiceName,
		Branch:      q.Branch,
		Status:      q.Status,
		Builder:     q.Builder,
		Limit:       limit,
		Offset:      q.Offset,
	}
}

func (q recentImagesQuery) requestLimit() int {
	if q.Limit <= 0 {
		return 10
	}
	return q.Limit
}
