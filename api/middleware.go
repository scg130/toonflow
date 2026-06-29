package api

import (
	"net/http"
	"strings"

	"toonflow/auth"

	"github.com/gin-gonic/gin"
)

func authToken(c *gin.Context) string {
	header := c.GetHeader("Authorization")
	if strings.HasPrefix(header, "Bearer ") {
		return strings.TrimSpace(header[7:])
	}
	if token, err := c.Cookie("toonflow_token"); err == nil {
		return token
	}
	return c.Query("token")
}

// AuthRequired validates session token and sets user context.
func AuthRequired(store *auth.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := authToken(c)
		sess, ok := store.Get(token)
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		c.Set("userID", sess.UserID)
		c.Set("username", sess.Username)
		c.Set("token", token)
		c.Next()
	}
}

func currentUserID(c *gin.Context) string {
	v, _ := c.Get("userID")
	s, _ := v.(string)
	return s
}
