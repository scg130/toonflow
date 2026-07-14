package media

import (
	"strings"

	"toonflow/task"
)

// ShouldChainVideoContinuity decides whether the next/current shot should start from
// the previous accepted clip's last frame (Seedance continuation gate).
//
// Chain when:
//   - scene_link is continuous, or
//   - same non-empty scene name as the previous shot (same-location soft cut still needs identity lock)
//
// Do not chain on hard scene changes (empty scene treated as unknown → only continuous links).
func ShouldChainVideoContinuity(sceneLink, scene, prevScene string) bool {
	link := strings.TrimSpace(strings.ToLower(sceneLink))
	if link == task.SceneLinkContinuous || link == "continuous" || link == "续接" || link == "衔接" {
		return true
	}
	scene = strings.TrimSpace(scene)
	prevScene = strings.TrimSpace(prevScene)
	if scene == "" || prevScene == "" {
		return false
	}
	return scene == prevScene
}
