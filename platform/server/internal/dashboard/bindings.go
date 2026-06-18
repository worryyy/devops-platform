package dashboard

import (
	"github.com/gin-gonic/gin"
	"github.com/worryyy/devops-platform/platform/server/internal/pkg/bizerr"
	"github.com/worryyy/devops-platform/platform/server/internal/pkg/responses"
)

type summaryQuery struct {
	Range       string `form:"range"`
	ServiceName string `form:"service"`
	Environment string `form:"environment"`
	Status      string `form:"status"`
}

func bindQuery(c *gin.Context, req any) bool {
	if err := c.ShouldBindQuery(req); err != nil {
		responses.Fail(c, bizerr.Param("invalid request parameters"))
		return false
	}
	return true
}

func (q summaryQuery) filter() SummaryFilter {
	return SummaryFilter{
		Range:       q.Range,
		ServiceName: q.ServiceName,
		Environment: q.Environment,
		Status:      q.Status,
	}
}
