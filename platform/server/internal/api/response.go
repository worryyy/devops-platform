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

func ErrorErr(status int, message string) error { return &httpError{status: status, message: message} }

var (
	errUnauthorized   = &httpError{status: http.StatusUnauthorized, message: "unauthorized"}
	errForbidden      = &httpError{status: http.StatusForbidden, message: "forbidden"}
	errNotFoundGeneric = &httpError{status: http.StatusNotFound, message: "not found"}
)

func respData(c *gin.Context, data any) {
	c.JSON(http.StatusOK, Response{Code: 0, Message: "ok", Data: data})
}

func respFail(c *gin.Context, err error) {
	var he *httpError
	if !errors.As(err, &he) {
		he = &httpError{status: http.StatusInternalServerError, message: "internal server error"}
	}
	c.JSON(he.status, Response{Code: he.status, Message: he.message})
	c.Error(err)
}
