package topic

import (
	apperrors "campus-forum/internal/pkg/errors"
	"campus-forum/internal/pkg/pagination"
	"campus-forum/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) Create(c *gin.Context) {
	var req CreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, apperrors.BadRequest("invalid request body"))
		return
	}

	topic, err := h.service.Create(c.Request.Context(), req)
	if err != nil {
		response.Fail(c, err)
		return
	}

	response.Success(c, topic)
}

func (h *Handler) List(c *gin.Context) {
	params := pagination.FromQuery(c)
	list, total, err := h.service.List(c.Request.Context(), params)
	if err != nil {
		response.Fail(c, err)
		return
	}

	response.Success(c, pagination.NewResult(list, params, total))
}

func (h *Handler) Detail(c *gin.Context) {
	topic, err := h.service.Detail(c.Request.Context(), c.Query("topic_id"))
	if err != nil {
		response.Fail(c, err)
		return
	}

	response.Success(c, topic)
}

func (h *Handler) Delete(c *gin.Context) {
	var req DeleteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, apperrors.BadRequest("invalid request body"))
		return
	}

	if err := h.service.Delete(c.Request.Context(), req); err != nil {
		response.Fail(c, err)
		return
	}

	response.OK(c)
}

func (h *Handler) Search(c *gin.Context) {
	params := pagination.FromQuery(c)
	list, total, err := h.service.Search(c.Request.Context(), c.Query("keyword"), params)
	if err != nil {
		response.Fail(c, err)
		return
	}

	response.Success(c, pagination.NewResult(list, params, total))
}
