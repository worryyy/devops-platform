package response

import (
	"net/http"

	apperrors "campus-forum/internal/pkg/errors"
	"github.com/gin-gonic/gin"
)

type Body struct {
	Code int         `json:"code"`
	Msg  string      `json:"msg"`
	Data interface{} `json:"data,omitempty"`
}

func Success(c *gin.Context, data interface{}) {
	c.JSON(http.StatusOK, Body{
		Code: http.StatusOK,
		Msg:  "success",
		Data: data,
	})
}

func OK(c *gin.Context) {
	Success(c, gin.H{})
}

func Fail(c *gin.Context, err error) {
	if appErr, ok := apperrors.AsAppError(err); ok {
		c.JSON(appErr.Status, Body{
			Code: appErr.Code,
			Msg:  appErr.Message,
		})
		return
	}

	c.JSON(http.StatusInternalServerError, Body{
		Code: http.StatusInternalServerError,
		Msg:  "internal server error",
	})
}
