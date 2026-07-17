// Package service re-exports the split subpackages for backward-compatible imports.
package service

import (
	svcagent "toonflow/service/agent"
	svcasset "toonflow/service/asset"
	svccore "toonflow/service/core"
	svcmedia "toonflow/service/media"
	svcpipeline "toonflow/service/pipeline"
	svcproject "toonflow/service/project"
	svcstoryboard "toonflow/service/storyboard"
	svctimeline "toonflow/service/timeline"
	svcvoice "toonflow/service/voice"
)

// --- agent ---
type (
	AgentChat        = svcagent.AgentChat
	ChatAction       = svcagent.ChatAction
	ChatActionIntent = svcagent.ChatActionIntent
	ChatResponse     = svcagent.ChatResponse
)

// --- project ---
type (
	SourceText    = svcproject.SourceText
	Episode       = svcproject.Episode
	EpisodeParams = svcproject.EpisodeParams
)

var (
	AnalyzeSourceEvents          = svcproject.AnalyzeSourceEvents
	SplitEpisodes                = svcproject.SplitEpisodes
	RefreshProjectStyleAnchor    = svcproject.RefreshProjectStyleAnchor
	LoadProjectStyleAnchor       = svcproject.LoadProjectStyleAnchor
	BuildStyleAnchor             = svcproject.BuildStyleAnchor
	BuildShotImagePrompt         = svcproject.BuildShotImagePrompt
	ResolutionToVideoRatio       = svcproject.ResolutionToVideoRatio
	IsContentPolicyViolation     = svcproject.IsContentPolicyViolation
	UserFacingImagePolicyMessage = svcproject.UserFacingImagePolicyMessage
	SanitizeImagePromptForPolicy = svcproject.SanitizeImagePromptForPolicy
)

const (
	SanitizeLevelLight  = svcproject.SanitizeLevelLight
	SanitizeLevelStrict = svcproject.SanitizeLevelStrict
)

func SanitizeWorkContent(text string) string { return svcagent.SanitizeWorkContent(text) }

// --- asset ---
var (
	ExtractAssetsFromEpisode       = svcasset.ExtractAssetsFromEpisode
	CountProjectAssets             = svcasset.CountProjectAssets
	RequireProjectAssets           = svcasset.RequireProjectAssets
	ShotImageParams                = svcasset.ShotImageParams
	SanitizeStoryboardItemForImage = svcasset.SanitizeStoryboardItemForImage
)

// --- storyboard ---
var (
	LoadStoryboardItems          = svcstoryboard.LoadStoryboardItems
	SaveStoryboardItems          = svcstoryboard.SaveStoryboardItems
	UpdateStoryboardShotDialogue = svcstoryboard.UpdateStoryboardShotDialogue
	NormalizeStoryboardItems     = svcstoryboard.NormalizeStoryboardItems
	StoryboardFromRecentChat     = svcstoryboard.StoryboardFromRecentChat
	MergeStoryboardMedia         = svcstoryboard.MergeStoryboardMedia
	ShotHasImage                 = svcstoryboard.ShotHasImage
	MergeShotMediaFromStore      = svcstoryboard.MergeShotMediaFromStore
	UpdateStoryboardShotMedia     = svcstoryboard.UpdateStoryboardShotMedia
	UpdateStoryboardShotKeyframes = svcstoryboard.UpdateStoryboardShotKeyframes
)

// --- media ---
type (
	ShotClip          = svcmedia.ShotClip
	BatchVideoOutcome = svcmedia.BatchVideoOutcome
	ComposeShotResult = svcmedia.ComposeShotResult
)

const DefaultShotDurationSec = svcmedia.DefaultShotDurationSec

var (
	ListShotClips               = svcmedia.ListShotClips
	GenerateShotClip            = svcmedia.GenerateShotClip
	GenerateShotClipsSequential = svcmedia.GenerateShotClipsSequential
	BatchVideoTaskTimeout       = svcmedia.BatchVideoTaskTimeout
	SelectShotClip              = svcmedia.SelectShotClip
	DeleteShotClip              = svcmedia.DeleteShotClip
	ComposeShotClip             = svcmedia.ComposeShotClip
	BatchComposeShots           = svcmedia.BatchComposeShots
	RequestShotImageWithRetry   = svcmedia.RequestShotImageWithRetry
	BuildBeatImagePrompt        = svcmedia.BuildBeatImagePrompt
	SortShotNumbers             = svcmedia.SortShotNumbers
	PreflightShotKeyframes      = svcmedia.PreflightShotKeyframes
	PreflightShotsKeyframes     = svcmedia.PreflightShotsKeyframes
)

type (
	KeyframeAnomaly         = svcmedia.KeyframeAnomaly
	KeyframePreflightReport = svcmedia.KeyframePreflightReport
)

const (
	KeyframeAnomalyBlock = svcmedia.KeyframeAnomalyBlock
	KeyframeAnomalyWarn  = svcmedia.KeyframeAnomalyWarn
	KeyframeAnomalyInfo  = svcmedia.KeyframeAnomalyInfo
)

// --- timeline ---
type (
	TimelineEdit     = svctimeline.TimelineEdit
	NarrationSegment = svctimeline.NarrationSegment
	NarrationPlan    = svctimeline.NarrationPlan
)

var (
	LoadTimeline                   = svctimeline.LoadTimeline
	SaveTimeline                   = svctimeline.SaveTimeline
	ReloadTimelineFromClips        = svctimeline.ReloadTimelineFromClips
	ClearTimeline                  = svctimeline.ClearTimeline
	ExportTimeline                 = svctimeline.ExportTimeline
	GenerateNarrationPlan          = svctimeline.GenerateNarrationPlan
	SynthesizeNarrationPlan        = svctimeline.SynthesizeNarrationPlan
	ApplyNarrationToTimeline       = svctimeline.ApplyNarrationToTimeline
	NormalizeNarrationSegments     = svctimeline.NormalizeNarrationSegments
	TimelineVideoDuration          = svctimeline.TimelineVideoDuration
	TimelineExportDuration         = svctimeline.TimelineExportDuration
	ResolveNarrationTargetDuration = svctimeline.ResolveNarrationTargetDuration
)

const DefaultNarrationVoice = svcvoice.DefaultNarrationVoice

// --- voice ---
var (
	EdgeVoiceCatalog      = svcvoice.EdgeVoiceCatalog
	AssignCharacterVoices = svcvoice.AssignCharacterVoices
	LookupCharacterVoice  = svcvoice.LookupCharacterVoice
	RolesHaveVoices       = svcvoice.RolesHaveVoices
)

// --- pipeline ---
type (
	EpisodePipelineStep   = svcpipeline.EpisodePipelineStep
	EpisodePipelineDeps   = svcpipeline.EpisodePipelineDeps
	EpisodePipelineResult = svcpipeline.EpisodePipelineResult
	EpisodePipelineRun    = svcpipeline.EpisodePipelineRun
	EpisodeStepStatus     = svcpipeline.EpisodeStepStatus
	PipelineUIState       = svcpipeline.PipelineUIState
)

var (
	PlanEpisodePipeline       = svcpipeline.PlanEpisodePipeline
	RunEpisodePipeline        = svcpipeline.RunEpisodePipeline
	ListEpisodePipelineStatus = svcpipeline.ListEpisodePipelineStatus
	EpisodeStepIDs            = svcpipeline.EpisodeStepIDs
	EpisodePipelineTimeout    = svcpipeline.EpisodePipelineTimeout
	EpisodePipelines          = svcpipeline.EpisodePipelines
		InitPipelineUIState       = svcpipeline.InitPipelineUIState
		AppendPipelineUIProgress  = svcpipeline.AppendPipelineUIProgress
		FinalizePipelineUIState   = svcpipeline.FinalizePipelineUIState
		RecoverStalePipelineUIStates = svcpipeline.RecoverStalePipelineUIStates
		ClearPipelineUIState      = svcpipeline.ClearPipelineUIState
		SetPipelineUIPaused       = svcpipeline.SetPipelineUIPaused
		ListPipelineUIStates      = svcpipeline.ListPipelineUIStates
	)

// --- core ---
type ProgressFunc = svccore.ProgressFunc
type PipelineStatus = svccore.PipelineStatus

const DefaultProgressHeartbeat = svccore.DefaultProgressHeartbeat

var (
	WithProgress              = svccore.WithProgress
	WithPipelineStatus        = svccore.WithPipelineStatus
	PipelineStatusFromContext = svccore.PipelineStatusFromContext
	WithStreamDelta           = svccore.WithStreamDelta
	WithStreamEnd             = svccore.WithStreamEnd
	WithStepProgress          = svccore.WithStepProgress
	WithPauseGate             = svccore.WithPauseGate
	InheritPipelineContext    = svccore.InheritPipelineContext
	ReportProgress            = svccore.ReportProgress
	ReportStepProgress        = svccore.ReportStepProgress
	StartProgressHeartbeat    = svccore.StartProgressHeartbeat
	EnsureProgressHeartbeat   = svccore.EnsureProgressHeartbeat
	WaitWithProgress          = svccore.WaitWithProgress
	WaitIfPaused              = svccore.WaitIfPaused
	NewPauseGate              = svccore.NewPauseGate
	EnrichTaskMeta            = svccore.EnrichTaskMeta
	MarkTaskFailed            = svccore.MarkTaskFailed
	UserMessage               = svccore.UserMessage
	UserMessageWithLogID      = svccore.UserMessageWithLogID
	UserMessageFromContext    = svccore.UserMessageFromContext
	AppendLogID               = svccore.AppendLogID
	UserMessageText           = svccore.UserMessageText
)
