package errors

import stderrors "errors"

type AppError struct {
	Status  int
	Code    int
	Message string
}

func (e *AppError) Error() string {
	return e.Message
}

func New(status int, message string) *AppError {
	return &AppError{Status: status, Code: status, Message: message}
}

func BadRequest(message string) *AppError {
	return New(400, message)
}

func Unauthorized(message string) *AppError {
	return New(401, message)
}

func Forbidden(message string) *AppError {
	return New(403, message)
}

func NotFound(message string) *AppError {
	return New(404, message)
}

func Conflict(message string) *AppError {
	return New(409, message)
}

func Internal(message string) *AppError {
	return New(500, message)
}

func AsAppError(err error) (*AppError, bool) {
	var appErr *AppError
	if stderrors.As(err, &appErr) {
		return appErr, true
	}
	return nil, false
}
