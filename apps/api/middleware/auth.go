package middleware

import (
	"errors"
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
		tokenStr, err := extractToken(c)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
			return
		}

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

// extractToken reads the bearer token from the Authorization header, falling
// back to a ?token= query param when no header is present. The fallback only
// matters for WebSocket routes (/ws, /ws/logs/:container) since browsers
// cannot set custom headers on the handshake request; every other client
// keeps using the header as normal.
func extractToken(c *gin.Context) (string, error) {
	authHeader := c.GetHeader("Authorization")
	if authHeader == "" {
		if q := c.Query("token"); q != "" {
			return q, nil
		}
		return "", errors.New("missing Authorization header")
	}

	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
		return "", errors.New("invalid Authorization format, expected: Bearer <token>")
	}
	return parts[1], nil
}
