package session

import (
	apperrors "campus-forum/internal/pkg/errors"
	"campus-forum/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) Login(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, apperrors.BadRequest("invalid request body"))
		return
	}

	user, err := h.service.Login(c.Request.Context(), req)
	if err != nil {
		response.Fail(c, err)
		return
	}

	response.Success(c, user)
}
