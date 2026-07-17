package media

import (
	"database/sql"
	"fmt"
	"os"
	"strings"

	"toonflow/adapter"
	"toonflow/service/internal/fsutil"
	"toonflow/service/storyboard"
	"toonflow/task"
)

// KeyframeAnomalySeverity controls whether video generation may proceed without confirmation.
type KeyframeAnomalySeverity string

const (
	KeyframeAnomalyBlock KeyframeAnomalySeverity = "block"
	KeyframeAnomalyWarn  KeyframeAnomalySeverity = "warn"
	KeyframeAnomalyInfo  KeyframeAnomalySeverity = "info"
)

// KeyframeAnomaly is one deterministic preflight finding (no AI vision / no regen).
type KeyframeAnomaly struct {
	ShotNumber int                     `json:"shot_number"`
	Code       string                  `json:"code"`
	Severity   KeyframeAnomalySeverity `json:"severity"`
	Message    string                  `json:"message"`
	Suggestion string                  `json:"suggestion,omitempty"`
}

// KeyframePreflightReport aggregates anomalies for one or more shots.
type KeyframePreflightReport struct {
	Passed    bool              `json:"passed"`
	Anomalies []KeyframeAnomaly `json:"anomalies"`
}

// NeedsManualConfirm reports whether any warn/block anomaly requires user confirmation.
func (r *KeyframePreflightReport) NeedsManualConfirm() bool {
	if r == nil {
		return false
	}
	for _, a := range r.Anomalies {
		if a.Severity == KeyframeAnomalyWarn || a.Severity == KeyframeAnomalyBlock {
			return true
		}
	}
	return false
}

// HasBlockers reports whether generation must not proceed even with confirmation.
func (r *KeyframePreflightReport) HasBlockers() bool {
	if r == nil {
		return false
	}
	for _, a := range r.Anomalies {
		if a.Severity == KeyframeAnomalyBlock {
			return true
		}
	}
	return false
}

// PreflightShotKeyframes checks existing storyboard metadata and on-disk PNGs.
// Zero extra AI calls — only filesystem + pixel heuristics.
func PreflightShotKeyframes(db *sql.DB, outputDir, projectID, episodeID string, shotNumber int) (*KeyframePreflightReport, error) {
	shot, err := storyboard.LoadShot(db, projectID, episodeID, shotNumber)
	if err != nil {
		return nil, err
	}
	item := shot.AsStoryboardItem()
	report := &KeyframePreflightReport{Passed: true}
	report.Anomalies = append(report.Anomalies, preflightShotMetadata(item)...)
	report.Anomalies = append(report.Anomalies, preflightShotFiles(outputDir, item)...)
	for i := range report.Anomalies {
		report.Anomalies[i].ShotNumber = shotNumber
	}
	report.Passed = !report.NeedsManualConfirm()
	return report, nil
}

// PreflightShotsKeyframes runs preflight for multiple shot numbers.
func PreflightShotsKeyframes(db *sql.DB, outputDir, projectID, episodeID string, shotNumbers []int) (*KeyframePreflightReport, error) {
	report := &KeyframePreflightReport{Passed: true}
	for _, n := range shotNumbers {
		one, err := PreflightShotKeyframes(db, outputDir, projectID, episodeID, n)
		if err != nil {
			return nil, fmt.Errorf("第 %d 镜预检失败: %w", n, err)
		}
		report.Anomalies = append(report.Anomalies, one.Anomalies...)
	}
	report.Passed = !report.NeedsManualConfirm()
	return report, nil
}

func preflightShotMetadata(item task.StoryboardItem) []KeyframeAnomaly {
	var out []KeyframeAnomaly
	if !storyboard.ShotHasAllBeatImages(item) {
		out = append(out, KeyframeAnomaly{
			Code:       "incomplete_keyframes",
			Severity:   KeyframeAnomalyBlock,
			Message:    "关键帧不完整，无法生成视频",
			Suggestion: "请先生成该镜全部关键帧",
		})
		return out
	}
	if len(item.Beats) < 2 {
		out = append(out, KeyframeAnomaly{
			Code:       "beats_lt_2",
			Severity:   KeyframeAnomalyBlock,
			Message:    "缺少时间节点方案（至少 2 个 beats）",
			Suggestion: "请重新生成分镜后再生关键帧",
		})
		return out
	}
	missingPrompt := 0
	for _, b := range item.Beats {
		if strings.TrimSpace(b.ImagePrompt) == "" {
			missingPrompt++
		}
	}
	if missingPrompt > 0 {
		out = append(out, KeyframeAnomaly{
			Code:       "missing_image_prompt",
			Severity:   KeyframeAnomalyWarn,
			Message:    fmt.Sprintf("%d 个关键帧缺少 image_prompt，将使用弱 fallback", missingPrompt),
			Suggestion: "建议重新生成分镜或关键帧",
		})
	}
	if HasLargeFramingJump(item.Beats) {
		out = append(out, KeyframeAnomaly{
			Code:       "large_framing_jump",
			Severity:   KeyframeAnomalyWarn,
			Message:    "关键帧景别跨度过大（如背影↔面部），视频易出现硬 morph",
			Suggestion: "确认后将用保守 frames2；或重做关键帧保持景别连贯",
		})
	}
	if !ActionContinueCompatibleWithBeats(item.ActionContinue, item.Beats) &&
		strings.TrimSpace(item.ActionContinue) != "" &&
		!isPlaceholderContinuity(item.ActionContinue) {
		out = append(out, KeyframeAnomaly{
			Code:       "action_continue_mismatch",
			Severity:   KeyframeAnomalyWarn,
			Message:    "承接动作与本镜关键帧内容冲突，已计划丢弃该承接文案",
			Suggestion: "可编辑分镜承接或重做关键帧",
		})
	}
	if HasLiquidSurfaceImpact(item.Beats) {
		out = append(out, KeyframeAnomaly{
			Code:       "liquid_impact",
			Severity:   KeyframeAnomalyWarn,
			Message:    "含液体砸落关键帧，易出现直立液柱等异常形态",
			Suggestion: "请确认末关键帧液体已平铺落定；异常可重生成关键帧",
		})
	}
	return out
}

func preflightShotFiles(outputDir string, item task.StoryboardItem) []KeyframeAnomaly {
	var out []KeyframeAnomaly
	paths := make([]string, 0, len(item.Beats))
	for i, b := range item.Beats {
		url := strings.TrimSpace(b.ImageURL)
		if url == "" {
			continue
		}
		if adapter.IsCDNImageURL(url) {
			continue // remote-only; cannot verify local file
		}
		local, ok := fsutil.PublicURLToLocal(outputDir, url)
		if !ok {
			out = append(out, KeyframeAnomaly{
				Code:       "invalid_image_url",
				Severity:   KeyframeAnomalyBlock,
				Message:    fmt.Sprintf("关键帧 %d URL 无法映射到本地文件", i+1),
				Suggestion: "请重新生成该关键帧",
			})
			continue
		}
		st, err := os.Stat(local)
		if err != nil || st.Size() == 0 {
			out = append(out, KeyframeAnomaly{
				Code:       "missing_image_file",
				Severity:   KeyframeAnomalyBlock,
				Message:    fmt.Sprintf("关键帧 %d 文件缺失或为空", i+1),
				Suggestion: "请重新生成该关键帧",
			})
			continue
		}
		if _, err := loadImageFile(local); err != nil {
			out = append(out, KeyframeAnomaly{
				Code:       "undecodable_image",
				Severity:   KeyframeAnomalyBlock,
				Message:    fmt.Sprintf("关键帧 %d 无法解码", i+1),
				Suggestion: "请重新生成该关键帧",
			})
			continue
		}
		paths = append(paths, local)
	}
	// Consecutive beat pixel jump — warn only when we have local decodable pairs.
	for i := 0; i+1 < len(paths); i++ {
		a, errA := loadImageFile(paths[i])
		b, errB := loadImageFile(paths[i+1])
		if errA != nil || errB != nil {
			continue
		}
		diff := avgPixelDiff(a, b)
		if diff > 0.55 {
			out = append(out, KeyframeAnomaly{
				Code:       "pixel_jump",
				Severity:   KeyframeAnomalyWarn,
				Message:    fmt.Sprintf("关键帧 %d→%d 画面差异过大（%.0f%%），视频易变形", i+1, i+2, diff*100),
				Suggestion: "确认后继续，或重做其中一帧使构图更连贯",
			})
		}
	}
	return out
}

// HasLargeFramingJump is exported for video mode + preflight.
func HasLargeFramingJump(beats []task.ShotBeat) bool {
	return hasLargeFramingJump(beats)
}

// ActionContinueCompatibleWithBeats is exported for preflight.
func ActionContinueCompatibleWithBeats(ac string, beats []task.ShotBeat) bool {
	return actionContinueCompatibleWithBeats(ac, beats)
}

// HasLiquidSurfaceImpact is exported for preflight.
func HasLiquidSurfaceImpact(beats []task.ShotBeat) bool {
	return hasLiquidSurfaceImpact(beats)
}
