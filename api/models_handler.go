package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"toonflow/adapter"

	"github.com/gin-gonic/gin"
)

func (r *Router) modelTestError(c *gin.Context, err error) {
	msg := userMsg(c, err)
	hint := ""
	if strings.Contains(msg, "401") || strings.Contains(msg, "无效的令牌") {
		hint = "API Key 无效。请在「设置 → 供应商」中编辑并填入 Agnes 控制台 (platform.agnes-ai.com) 的密钥，不要填 API 地址。"
	}
	cfg := adapter.ResolveConfigFromDB(r.db.DB)
	c.JSON(http.StatusOK, gin.H{
		"ok":     false,
		"error":  msg,
		"hint":   hint,
		"vendor": cfg.Info,
	})
}

func (r *Router) modelTestTextHandler(c *gin.Context) {
	var req struct {
		Model  string `json:"model"`
		Prompt string `json:"prompt"`
	}
	_ = c.ShouldBindJSON(&req)
	if req.Model == "" {
		req.Model = adapter.DefaultTextModel
	}
	if req.Prompt == "" {
		req.Prompt = "Reply with exactly: ToonFlow text model OK"
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()

	cfg := adapter.ResolveConfigFromDB(r.db.DB)
	if cfg.APIKey == "" {
		c.JSON(http.StatusOK, gin.H{
			"ok":    false,
			"error": "API key not configured",
			"hint":  "请在「设置 → 供应商」添加 Agnes-AI 并填写正确的 API Key（不是 URL）",
		})
		return
	}

	v := adapter.NewAgnesAIVendor(cfg.BaseURL, cfg.APIKey)
	resp, err := v.TextRequest(ctx, req.Model, adapter.TextParams{
		Messages: []adapter.TextMessage{
			{Role: "user", Content: req.Prompt},
		},
		MaxTokens: 128,
	})
	if err != nil {
		r.modelTestError(c, err)
		return
	}

	content := resp.Content
	if len(content) > 200 {
		content = content[:200] + "..."
	}
	c.JSON(http.StatusOK, gin.H{
		"ok":      true,
		"model":   resp.Model,
		"content": content,
	})
}

func (r *Router) modelTestImageHandler(c *gin.Context) {
	var req struct {
		Model  string `json:"model"`
		Prompt string `json:"prompt"`
	}
	_ = c.ShouldBindJSON(&req)
	if req.Model == "" {
		req.Model = adapter.DefaultImageModel
	}
	if req.Prompt == "" {
		req.Prompt = "A simple red circle on white background, minimal test image"
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 120*time.Second)
	defer cancel()

	cfg := adapter.ResolveConfigFromDB(r.db.DB)
	if cfg.APIKey == "" {
		c.JSON(http.StatusOK, gin.H{
			"ok":    false,
			"error": "API key not configured",
			"hint":  "请在「设置 → 供应商」添加 Agnes-AI 并填写正确的 API Key（不是 URL）",
		})
		return
	}

	v := adapter.NewAgnesAIVendor(cfg.BaseURL, cfg.APIKey)
	resp, err := v.ImageRequest(ctx, req.Model, adapter.ImageParams{
		Prompt:      req.Prompt,
		Model:       req.Model,
		AspectRatio: "1:1",
	})
	if err != nil {
		r.modelTestError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":         true,
		"model":      resp.Model,
		"data_url":   resp.DataURL,
		"remote_url": resp.RemoteURL,
	})
}

func (r *Router) modelTestVideoHandler(c *gin.Context) {
	var req struct {
		Model  string `json:"model"`
		Prompt string `json:"prompt"`
	}
	_ = c.ShouldBindJSON(&req)
	if req.Model == "" {
		req.Model = adapter.DefaultVideoModel
	}
	if req.Prompt == "" {
		req.Prompt = "A calm ocean wave, 2 second test clip"
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 180*time.Second)
	defer cancel()

	cfg := adapter.ResolveConfigFromDB(r.db.DB)
	if cfg.APIKey == "" {
		c.JSON(http.StatusOK, gin.H{
			"ok":    false,
			"error": "API key not configured",
			"hint":  "请在「设置 → 供应商」添加 Agnes-AI 并填写正确的 API Key（不是 URL）",
		})
		return
	}

	v := adapter.NewAgnesAIVendor(cfg.BaseURL, cfg.APIKey)
	resp, err := v.VideoRequest(ctx, req.Model, adapter.VideoParams{
		Prompt:   req.Prompt,
		Model:    req.Model,
		Duration: 2,
	})
	if err != nil {
		r.modelTestError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":        true,
		"model":     resp.Model,
		"video_url": resp.VideoURL,
	})
}
