package api

import (
	"context"
	"net/http"
	"strconv"
	"time"

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
	episodeID := c.Param("epId")
	shotNum, err := strconv.Atoi(c.Param("shotNum"))
	if err != nil || shotNum <= 0 {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "invalid shot number"})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Minute)
	defer cancel()

	clip, err := service.GenerateShotClip(ctx, r.db.DB, r.resolveVendor(), r.outputDir, projectID, episodeID, shotNum)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, clip)
}

func (r *Router) shotClipSelectHandler(c *gin.Context) {
	clipID := c.Param("clipId")
	if err := service.SelectShotClip(r.db.DB, clipID); err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (r *Router) shotClipDeleteHandler(c *gin.Context) {
	clipID := c.Param("clipId")
	if err := service.DeleteShotClip(r.db.DB, r.outputDir, clipID); err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
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

func (r *Router) timelineSaveHandler(c *gin.Context) {
	projectID, ok := r.requireProject(c)
	if !ok {
		return
	}
	var tl service.TimelineEdit
	if err := c.ShouldBindJSON(&tl); err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
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
		EpisodeID string              `json:"episode_id" binding:"required"`
		Timeline  *service.TimelineEdit `json:"timeline"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
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
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"video_url": url})
}
