package pipeline

import (
	"context"
	"fmt"
	"sync"

	"toonflow/service/core"
)

// EpisodePipelineRegistry tracks active episode pipeline runs per project+episode.
type EpisodePipelineRegistry struct {
	mu   sync.Mutex
	runs map[string]*EpisodePipelineRun
}

// EpisodePipelineRun is one in-flight episode automation.
type EpisodePipelineRun struct {
	ID        string
	ProjectID string
	EpisodeID string
	UserID    string
	Gate      *core.PauseGate
	Cancel    context.CancelFunc
}

// Global episode pipeline registry.
var EpisodePipelines = &EpisodePipelineRegistry{runs: make(map[string]*EpisodePipelineRun)}

func episodeRunKey(projectID, episodeID string) string {
	return projectID + ":" + episodeID
}

// Register adds a run; returns error if one is already active.
func (r *EpisodePipelineRegistry) Register(run *EpisodePipelineRun) error {
	if r == nil || run == nil {
		return fmt.Errorf("invalid pipeline run")
	}
	key := episodeRunKey(run.ProjectID, run.EpisodeID)
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.runs[key]; ok {
		return fmt.Errorf("该分集已有流水线在执行中")
	}
	r.runs[key] = run
	return nil
}

// Unregister removes a finished run.
func (r *EpisodePipelineRegistry) Unregister(projectID, episodeID string) {
	if r == nil {
		return
	}
	key := episodeRunKey(projectID, episodeID)
	r.mu.Lock()
	delete(r.runs, key)
	r.mu.Unlock()
}

// Get returns the active run for a project episode.
func (r *EpisodePipelineRegistry) Get(projectID, episodeID string) *EpisodePipelineRun {
	if r == nil {
		return nil
	}
	key := episodeRunKey(projectID, episodeID)
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.runs[key]
}

// PauseRun pauses the active run for project+episode.
func (r *EpisodePipelineRegistry) PauseRun(projectID, episodeID string) error {
	run := r.Get(projectID, episodeID)
	if run == nil || run.Gate == nil {
		return fmt.Errorf("当前没有正在执行的流水线")
	}
	run.Gate.Pause()
	return nil
}

// ResumeRun resumes the active run.
func (r *EpisodePipelineRegistry) ResumeRun(projectID, episodeID string) error {
	run := r.Get(projectID, episodeID)
	if run == nil || run.Gate == nil {
		return fmt.Errorf("当前没有正在执行的流水线")
	}
	run.Gate.Resume()
	return nil
}

// CancelRun cancels the active run context.
func (r *EpisodePipelineRegistry) CancelRun(projectID, episodeID string) error {
	run := r.Get(projectID, episodeID)
	if run == nil || run.Cancel == nil {
		return fmt.Errorf("当前没有正在执行的流水线")
	}
	run.Cancel()
	return nil
}

// ActivePipelineInfo summarizes a running episode pipeline for the UI.
type ActivePipelineInfo struct {
	EpisodeID string `json:"episode_id"`
	Paused    bool   `json:"paused"`
}

// ListByProject returns active pipeline runs for a project.
func (r *EpisodePipelineRegistry) ListByProject(projectID string) []ActivePipelineInfo {
	if r == nil || projectID == "" {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []ActivePipelineInfo
	for _, run := range r.runs {
		if run == nil || run.ProjectID != projectID {
			continue
		}
		paused := run.Gate != nil && run.Gate.IsPaused()
		out = append(out, ActivePipelineInfo{EpisodeID: run.EpisodeID, Paused: paused})
	}
	return out
}
