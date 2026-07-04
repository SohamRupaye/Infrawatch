package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// CORS returns a Gin middleware that sets CORS headers.
// allowOrigins is the list from config; if empty, "*" is used for dev convenience.
func CORS(allowOrigins []string) gin.HandlerFunc {
	originSet := make(map[string]struct{}, len(allowOrigins))
	for _, o := range allowOrigins {
		originSet[strings.TrimRight(o, "/")] = struct{}{}
	}
	allowAll := len(allowOrigins) == 0

	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")

		allowed := ""
		if allowAll {
			allowed = "*"
		} else if _, ok := originSet[origin]; ok {
			allowed = origin
		}

		if allowed != "" {
			c.Header("Access-Control-Allow-Origin", allowed)
			c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			c.Header("Access-Control-Allow-Headers", "Origin, Content-Type, Authorization, X-Requested-With")
			c.Header("Access-Control-Expose-Headers", "Content-Length")
			c.Header("Access-Control-Allow-Credentials", "true")
			c.Header("Vary", "Origin")
		}

		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}
