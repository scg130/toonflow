package api

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"toonflow/service"
)

func (r *Router) storyboardShotUpdateHandler(c *gin.Context) {
	projectID, ok := r.requireProject(c)
	if !ok {
		return
	}
	episodeID := c.Param("epId")
	shotNum, err := strconv.Atoi(c.Param("shotNum"))
	if err != nil || shotNum <= 0 {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "invalid shot number"})
		return
	}
	var body struct {
		Dialogue string `json:"dialogue"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	if err := service.UpdateStoryboardShotDialogue(r.db.DB, projectID, episodeID, shotNum, body.Dialogue); err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": userMsg(c, err)})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "shot_number": shotNum, "dialogue": body.Dialogue})
}
