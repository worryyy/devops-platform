package api

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
)

// Response is the unified envelope for everything under /api. Health probes
// keep their bare shape (see routes_test.go).
type Response struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data"`
}

// httpError marks a handler error that already knows its HTTP status.
type httpError struct {
	status  int
	message string
}

func (e *httpError) Error() string { return e.message }

// ErrorErr builds an error carrying its HTTP status for RespFail.
func ErrorErr(status int, message string) error { return &httpError{status: status, message: message} }

// RespData writes a success envelope.
func RespData(c *gin.Context, data any) {
	c.JSON(http.StatusOK, Response{Code: 0, Message: "ok", Data: data})
}

// RespFail maps an error to the envelope; ErrorErr controls the status,
// anything else becomes a 500.
func RespFail(c *gin.Context, err error) {
	var he *httpError
	if !errors.As(err, &he) {
		he = &httpError{status: http.StatusInternalServerError, message: "internal server error"}
	}
	c.JSON(he.status, Response{Code: he.status, Message: he.message})
	c.Error(err)
}
