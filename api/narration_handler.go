package api

import (
	"net/http"

	"toonflow/service"

	"github.com/gin-gonic/gin"
)

func (r *Router) narrationPlanHandler(c *gin.Context) {
	projectID, ok := r.requireProject(c)
	if !ok {
		return
	}
	var req struct {
		EpisodeID string `json:"episode_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tl, err := service.LoadTimeline(r.db.DB, projectID, req.EpisodeID)
	if err != nil {
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}
	plan, err := service.GenerateNarrationPlan(c.Request.Context(), r.db.DB, r.resolveVendor(), projectID, req.EpisodeID, tl)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	tl.Narration = plan
	if err := service.SaveTimeline(r.db.DB, tl); err != nil {
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"narration": plan, "timeline": tl})
}

func (r *Router) narrationSynthesizeHandler(c *gin.Context) {
	projectID, ok := r.requireProject(c)
	if !ok {
		return
	}
	var req struct {
		EpisodeID string                    `json:"episode_id" binding:"required"`
		Segments  []service.NarrationSegment `json:"segments,omitempty"`
		Voice     string                    `json:"voice,omitempty"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tl, err := service.LoadTimeline(r.db.DB, projectID, req.EpisodeID)
	if err != nil {
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}
	plan := tl.Narration
	if plan == nil {
		plan = &service.NarrationPlan{
			ProjectID:     projectID,
			EpisodeID:     req.EpisodeID,
			TotalDuration: service.TimelineVideoDuration(tl),
			Voice:         service.DefaultNarrationVoice,
			Status:        "draft",
		}
	}
	if len(req.Segments) > 0 {
		plan.Segments = req.Segments
	}
	if req.Voice != "" {
		plan.Voice = req.Voice
	}
	if plan.TotalDuration <= 0 {
		plan.TotalDuration = service.TimelineVideoDuration(tl)
	}
	if len(plan.Segments) == 0 {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "请先生成旁白方案"})
		return
	}
	service.NormalizeNarrationSegments(plan.Segments, plan.TotalDuration)

	if err := service.SynthesizeNarrationPlan(c.Request.Context(), r.resolveVendor(), r.outputDir, plan); err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := service.ApplyNarrationToTimeline(tl, plan); err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := service.SaveTimeline(r.db.DB, tl); err != nil {
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"narration": plan, "timeline": tl})
}
