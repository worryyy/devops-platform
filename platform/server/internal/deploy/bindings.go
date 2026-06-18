package deploy

import (
	"github.com/gin-gonic/gin"
	"github.com/worryyy/devops-platform/platform/server/internal/pkg/bizerr"
	"github.com/worryyy/devops-platform/platform/server/internal/pkg/responses"
)

type deployURI struct {
	DeployID string `uri:"deployId" binding:"required"`
}

type deployListQuery struct {
	ServiceName string `form:"service"`
	Environment string `form:"environment"`
	Status      Status `form:"status"`
	Deployer    string `form:"deployer"`
	Limit       int    `form:"limit"`
	Offset      int    `form:"offset"`
}

type locksQuery struct {
	ServiceName string `form:"service"`
	Environment string `form:"environment"`
}

type actorRequest struct {
	Deployer string `json:"deployer"`
	Operator string `json:"operator"`
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

func (q deployListQuery) filter() ListFilter {
	limit := q.Limit
	if limit <= 0 {
		limit = 50
	}
	return ListFilter{
		ServiceName: q.ServiceName,
		Environment: q.Environment,
		Status:      q.Status,
		Deployer:    q.Deployer,
		Limit:       limit,
		Offset:      q.Offset,
	}
}

func (r actorRequest) actor() string {
	if r.Deployer != "" {
		return r.Deployer
	}
	return r.Operator
}
