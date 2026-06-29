package api

import (
	"net/http"

	"toonflow/auth"

	"github.com/gin-gonic/gin"
)

func (r *Router) loginHandler(c *gin.Context) {
	var req struct {
		Username string `json:"username" binding:"required"`
		Password string `json:"password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "username and password required"})
		return
	}

	userID, username, err := auth.Authenticate(r.db.DB, req.Username, req.Password)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid username or password"})
		return
	}

	token := r.sessions.Create(userID, username)
	c.SetCookie("toonflow_token", token, 86400, "/", "", false, true)
	c.JSON(http.StatusOK, gin.H{
		"token":    token,
		"user_id":  userID,
		"username": username,
	})
}

func (r *Router) meHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"user_id":  currentUserID(c),
		"username": c.GetString("username"),
	})
}

func (r *Router) logoutHandler(c *gin.Context) {
	if token, ok := c.Get("token"); ok {
		if t, ok := token.(string); ok {
			r.sessions.Delete(t)
		}
	}
	c.SetCookie("toonflow_token", "", -1, "/", "", false, true)
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}
