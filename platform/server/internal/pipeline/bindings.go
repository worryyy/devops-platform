package pipeline

import (
	"github.com/gin-gonic/gin"
	"github.com/worryyy/devops-platform/platform/server/internal/pkg/bizerr"
	"github.com/worryyy/devops-platform/platform/server/internal/pkg/responses"
)

type stageListQuery struct {
	BuildID  string `form:"build_id"`
	DeployID string `form:"deploy_id"`
}

func bindQuery(c *gin.Context, req any) bool {
	if err := c.ShouldBindQuery(req); err != nil {
		responses.Fail(c, bizerr.Param("invalid request parameters"))
		return false
	}
	return true
}

func (q stageListQuery) filter() ListFilter {
	return ListFilter{BuildID: q.BuildID, DeployID: q.DeployID}
}
