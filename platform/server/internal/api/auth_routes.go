package api

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/worryyy/devops-platform/platform/server/internal/auth"
)

type loginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

type loginResponse struct {
	Token     string     `json:"token"`
	ExpiresAt time.Time  `json:"expiresAt"`
	User      publicUser `json:"user"`
}

type publicUser struct {
	Username string `json:"username"`
	Role     string `json:"role"`
}

type AuthHandlers struct {
	Users  Authenticator
	Secret string
	Now    func() time.Time
}

func (h AuthHandlers) Register(router *gin.RouterGroup) {
	router.POST("/auth/login", h.login)
	router.GET("/auth/me", h.me)
}

func (h AuthHandlers) login(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		RespFail(c, ErrorErr(http.StatusBadRequest, "username and password are required"))
		return
	}
	user, err := h.Users.Authenticate(c.Request.Context(), req.Username, req.Password)
	if err != nil {
		// Same status for unknown user and wrong password.
		RespFail(c, ErrorErr(http.StatusUnauthorized, "invalid username or password"))
		return
	}
	now := h.Now()
	token, err := auth.SignToken(h.Secret, user.Username, user.Role, now)
	if err != nil {
		RespFail(c, ErrorErr(http.StatusInternalServerError, "sign token failed"))
		return
	}
	RespData(c, loginResponse{
		Token:     token,
		ExpiresAt: now.Add(auth.TokenTTL),
		User:      publicUser{Username: user.Username, Role: user.Role},
	})
}

func (h AuthHandlers) me(c *gin.Context) {
	claims := MustClaims(c)
	RespData(c, publicUser{Username: claims.Username, Role: claims.Role})
}
