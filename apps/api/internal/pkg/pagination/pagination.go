package pagination

import (
	"strconv"

	"github.com/gin-gonic/gin"
)

const (
	DefaultPage     = 1
	DefaultPageSize = 10
	MaxPageSize     = 100
)

type Params struct {
	Page     int
	PageSize int
}

type Result struct {
	List     interface{} `json:"list"`
	Page     int         `json:"page"`
	PageSize int         `json:"page_size"`
	Total    int64       `json:"total"`
}

func FromQuery(c *gin.Context) Params {
	page := parsePositiveInt(c.Query("page"), DefaultPage)
	pageSize := parsePositiveInt(c.Query("page_size"), DefaultPageSize)
	if pageSize > MaxPageSize {
		pageSize = MaxPageSize
	}
	return Params{Page: page, PageSize: pageSize}
}

func (p Params) Offset() int64 {
	return int64((p.Page - 1) * p.PageSize)
}

func (p Params) Limit() int64 {
	return int64(p.PageSize)
}

func NewResult(list interface{}, p Params, total int64) Result {
	return Result{
		List:     list,
		Page:     p.Page,
		PageSize: p.PageSize,
		Total:    total,
	}
}

func EmptyResult(p Params) Result {
	return NewResult([]interface{}{}, p, 0)
}

func parsePositiveInt(raw string, fallback int) int {
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}
