package user

import (
	"context"
	"strconv"

	apperrors "campus-forum/internal/pkg/errors"
	"campus-forum/internal/pkg/pagination"
	"campus-forum/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

type TopicLister interface {
	ListByAuthor(ctx context.Context, userID uint64, p pagination.Params) (interface{}, int64, error)
}

type CommentLister interface {
	ListByAuthor(ctx context.Context, userID uint64, p pagination.Params) (interface{}, int64, error)
}

type Handler struct {
	service       *Service
	topicLister   TopicLister
	commentLister CommentLister
}

func NewHandler(service *Service, topicLister TopicLister, commentLister CommentLister) *Handler {
	return &Handler{
		service:       service,
		topicLister:   topicLister,
		commentLister: commentLister,
	}
}

func (h *Handler) Register(c *gin.Context) {
	var req RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, apperrors.BadRequest("invalid request body"))
		return
	}

	user, err := h.service.Register(c.Request.Context(), req)
	if err != nil {
		response.Fail(c, err)
		return
	}

	response.Success(c, user)
}

func (h *Handler) Detail(c *gin.Context) {
	userID, err := parseUserIDQuery(c)
	if err != nil {
		response.Fail(c, err)
		return
	}

	user, err := h.service.GetPublicUser(c.Request.Context(), userID)
	if err != nil {
		response.Fail(c, err)
		return
	}

	response.Success(c, user)
}

func (h *Handler) TopicList(c *gin.Context) {
	if h.topicLister == nil {
		response.Fail(c, apperrors.Internal("topic service is not configured"))
		return
	}

	userID, err := parseUserIDQuery(c)
	if err != nil {
		response.Fail(c, err)
		return
	}

	params := pagination.FromQuery(c)
	list, total, err := h.topicLister.ListByAuthor(c.Request.Context(), userID, params)
	if err != nil {
		response.Fail(c, err)
		return
	}

	response.Success(c, pagination.NewResult(list, params, total))
}

func (h *Handler) CommentList(c *gin.Context) {
	if h.commentLister == nil {
		response.Fail(c, apperrors.Internal("comment service is not configured"))
		return
	}

	userID, err := parseUserIDQuery(c)
	if err != nil {
		response.Fail(c, err)
		return
	}

	params := pagination.FromQuery(c)
	list, total, err := h.commentLister.ListByAuthor(c.Request.Context(), userID, params)
	if err != nil {
		response.Fail(c, err)
		return
	}

	response.Success(c, pagination.NewResult(list, params, total))
}

func parseUserIDQuery(c *gin.Context) (uint64, error) {
	raw := c.Query("user_id")
	if raw == "" {
		return 0, apperrors.BadRequest("user_id is required")
	}

	value, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || value == 0 {
		return 0, apperrors.BadRequest("invalid user_id")
	}

	return value, nil
}
