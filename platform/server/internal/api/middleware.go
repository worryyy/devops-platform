package api

import (
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/worryyy/devops-platform/platform/server/internal/auth"
)

const claimsKey = "auth_claims"

// JWTAuth validates the Bearer token on every request and stores the claims
// for handlers. Routes registered on the public router skip it.
func JWTAuth(secret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		token, ok := strings.CutPrefix(header, "Bearer ")
		if !ok || token == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, Response{Code: http.StatusUnauthorized, Message: "missing bearer token"})
			return
		}
		claims, err := auth.ParseToken(secret, token)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, Response{Code: http.StatusUnauthorized, Message: "invalid or expired token"})
			return
		}
		c.Set(claimsKey, claims)
		c.Next()
	}
}

// RequireRole gates a route to specific roles.
func RequireRole(role string) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, ok := c.Get(claimsKey)
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, Response{Code: http.StatusUnauthorized, Message: "unauthorized"})
			return
		}
		if claims.(auth.Claims).Role != role {
			c.AbortWithStatusJSON(http.StatusForbidden, Response{Code: http.StatusForbidden, Message: "forbidden"})
			return
		}
		c.Next()
	}
}

func MustClaims(c *gin.Context) auth.Claims {
	return c.MustGet(claimsKey).(auth.Claims)
}

// RequestLog emits one structured line per request at info level.
func RequestLog(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		logger.Info("http",
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"status", c.Writer.Status(),
			"duration_ms", time.Since(start).Milliseconds(),
			"peer", c.ClientIP(),
		)
	}
}
