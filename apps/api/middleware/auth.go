package middleware

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"go.uber.org/zap"
)

// Logger returns a Gin middleware that logs each request with zap.
func Logger(logger *zap.Logger) gin.HandlerFunc {
	sugar := logger.Sugar()
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		sugar.Infow("request",
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"status", c.Writer.Status(),
			"latency_ms", time.Since(start).Milliseconds(),
			"ip", c.ClientIP(),
		)
	}
}

// JWT returns a Gin middleware that validates a Bearer JWT token.
// The secret is the raw HMAC signing key. If the token is valid, the
// "subject" claim is stored in the context as "user_id".
func JWT(secret string) gin.HandlerFunc {
	key := []byte(secret)
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing Authorization header"})
			return
		}

		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid Authorization format, expected: Bearer <token>"})
			return
		}
		tokenStr := parts[1]

		token, err := jwt.ParseWithClaims(
			tokenStr,
			&jwt.RegisteredClaims{},
			func(t *jwt.Token) (interface{}, error) {
				if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
					return nil, jwt.ErrSignatureInvalid
				}
				return key, nil
			},
		)
		if err != nil || !token.Valid {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired token"})
			return
		}

		if claims, ok := token.Claims.(*jwt.RegisteredClaims); ok {
			c.Set("user_id", claims.Subject)
		}
		c.Next()
	}
}
