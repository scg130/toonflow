package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func (r *Router) generateImagesHandler(c *gin.Context) {
	projectID, ok := r.requireProject(c)
	if !ok {
		return
	}
	var req struct {
		EpisodeID   string `json:"episode_id" binding:"required"`
		ShotNumbers []int  `json:"shot_numbers" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "episode_id 与 shot_numbers 必填"})
		return
	}
	if len(req.ShotNumbers) == 0 {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "请指定要生成的镜号"})
		return
	}

	t, err := r.submitImageGenerationTask(currentUserID(c), projectID, req.EpisodeID, req.ShotNumbers)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{
		"task_id":        t.ID,
		"title":          t.Title,
		"state":          t.State,
		"project_id":     t.ProjectID,
		"episode_id":     t.EpisodeID,
		"generate_shots": t.GenerateShots,
	})
}
