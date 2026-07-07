package api

import (
	"net/http"
	"strconv"

	"toonflow/service"

	"github.com/gin-gonic/gin"
)

func (r *Router) shotClipsListHandler(c *gin.Context) {
	projectID, ok := r.requireProject(c)
	if !ok {
		return
	}
	episodeID := c.Query("episode_id")
	if episodeID == "" {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "episode_id required"})
		return
	}
	clips, err := service.ListShotClips(r.db.DB, projectID, episodeID)
	if err != nil {
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, clips)
}

func (r *Router) shotClipGenerateHandler(c *gin.Context) {
	projectID, ok := r.requireProject(c)
	if !ok {
		return
	}
	userID := currentUserID(c)
	episodeID := c.Param("epId")
	shotNum, err := strconv.Atoi(c.Param("shotNum"))
	if err != nil || shotNum <= 0 {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "invalid shot number"})
		return
	}

	t, err := r.submitShotVideoTask(userID, projectID, episodeID, shotNum)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": userMsg(c, err)})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{
		"task_id":        t.ID,
		"title":          t.Title,
		"state":          t.State,
		"project_id":     t.ProjectID,
		"episode_id":     t.EpisodeID,
		"shot_number":    shotNum,
		"generate_shots": t.GenerateShots,
	})
}

func (r *Router) shotClipSelectHandler(c *gin.Context) {
	clipID := c.Param("clipId")
	if err := service.SelectShotClip(r.db.DB, clipID); err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": userMsg(c, err)})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (r *Router) shotClipDeleteHandler(c *gin.Context) {
	clipID := c.Param("clipId")
	if err := service.DeleteShotClip(r.db.DB, r.outputDir, clipID); err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": userMsg(c, err)})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (r *Router) timelineGetHandler(c *gin.Context) {
	projectID, ok := r.requireProject(c)
	if !ok {
		return
	}
	episodeID := c.Query("episode_id")
	if episodeID == "" {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "episode_id required"})
		return
	}
	tl, err := service.LoadTimeline(r.db.DB, projectID, episodeID)
	if err != nil {
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, tl)
}

func (r *Router) timelineReloadHandler(c *gin.Context) {
	projectID, ok := r.requireProject(c)
	if !ok {
		return
	}
	episodeID := c.Query("episode_id")
	if episodeID == "" {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "episode_id required"})
		return
	}
	tl, err := service.ReloadTimelineFromClips(r.db.DB, projectID, episodeID)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": userMsg(c, err)})
		return
	}
	c.JSON(http.StatusOK, tl)
}

func (r *Router) timelineClearHandler(c *gin.Context) {
	projectID, ok := r.requireProject(c)
	if !ok {
		return
	}
	episodeID := c.Query("episode_id")
	if episodeID == "" {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "episode_id required"})
		return
	}
	tl, err := service.ClearTimeline(r.db.DB, projectID, episodeID)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": userMsg(c, err)})
		return
	}
	c.JSON(http.StatusOK, tl)
}

func (r *Router) timelineSaveHandler(c *gin.Context) {
	projectID, ok := r.requireProject(c)
	if !ok {
		return
	}
	var tl service.TimelineEdit
	if err := c.ShouldBindJSON(&tl); err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": userMsg(c, err)})
		return
	}
	tl.ProjectID = projectID
	if tl.EpisodeID == "" {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "episode_id required"})
		return
	}
	if err := service.SaveTimeline(r.db.DB, &tl); err != nil {
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (r *Router) timelineExportHandler(c *gin.Context) {
	projectID, ok := r.requireProject(c)
	if !ok {
		return
	}
	var req struct {
		EpisodeID string                `json:"episode_id" binding:"required"`
		Timeline  *service.TimelineEdit `json:"timeline"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": userMsg(c, err)})
		return
	}
	tl := req.Timeline
	if tl == nil {
		var err error
		tl, err = service.LoadTimeline(r.db.DB, projectID, req.EpisodeID)
		if err != nil {
			c.AbortWithError(http.StatusInternalServerError, err)
			return
		}
	}
	tl.ProjectID = projectID
	tl.EpisodeID = req.EpisodeID
	url, err := service.ExportTimeline(r.outputDir, tl)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": userMsg(c, err)})
		return
	}
	if err := service.SaveTimeline(r.db.DB, tl); err != nil {
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"video_url":          url,
		"duration":           tl.ExportedDuration,
		"exported_video_url": tl.ExportedVideoURL,
		"timeline":           tl,
	})
}

func (r *Router) shotClipComposeHandler(c *gin.Context) {
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
	result, err := service.ComposeShotClip(c.Request.Context(), r.db.DB, r.resolveVendor(), r.outputDir, projectID, episodeID, shotNum)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": userMsg(c, err)})
		return
	}
	c.JSON(http.StatusOK, result)
}
