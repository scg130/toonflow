package task

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

// State represents the current state of a task.
type State string

const (
	StateWaiting    State = "waiting"
	StateParsing    State = "parsing"
	StateStoryboard State = "storyboarding"
	StateDrawing    State = "drawing"
	StateVideoGen   State = "video_gen"
	StateMerging    State = "merging"
	StateDone       State = "done"
	StateError      State = "error"
)

// Task represents a single generation job.
type Task struct {
	ID            string           `json:"id"`
	ProjectID     string           `json:"project_id,omitempty"`
	UserID        string           `json:"user_id"`
	Title         string           `json:"title,omitempty"`
	ProjectName   string           `json:"project_name,omitempty"`
	EpisodeTitle  string           `json:"episode_title,omitempty"`
	EpisodeNum    int              `json:"episode_num,omitempty"`
	Script        string           `json:"-"`
	Style         string           `json:"style"`
	Mode          string           `json:"mode,omitempty"` // full, parse, images, video
	EpisodeID     string           `json:"episode_id,omitempty"`
	GenerateShots      []int `json:"generate_shots,omitempty"`       // empty = all shots
	SkipExistingImages bool  `json:"skip_existing_images,omitempty"` // batch: skip shots that already have images
	FrameDuration float64          `json:"frame_duration"`
	Resolution    string           `json:"resolution"`
	FPS           int              `json:"fps"`
	State         State            `json:"state"`
	Progress      float32          `json:"progress"`
	Step          string           `json:"step"`
	ErrorMessage  string           `json:"error_message,omitempty"`
	CreatedAt     time.Time        `json:"created_at"`
	UpdatedAt     time.Time        `json:"updated_at"`

	Storyboard []StoryboardItem  `json:"storyboard,omitempty"`
	Images     []ImageArtifact   `json:"images,omitempty"`
	VideoPath  string            `json:"video_path,omitempty"`

	RetryCount  int
	MaxRetries  int
	Timeout     time.Duration
	cancel      context.CancelFunc
	ctx         context.Context
	mu          sync.RWMutex
}

// StoryboardItem represents a single shot in the storyboard.
type StoryboardItem struct {
	ShotNumber     int     `json:"shot_number"`
	Scene          string  `json:"scene"`
	Description    string  `json:"description"`
	Camera         string  `json:"camera"`
	Duration       float64 `json:"duration"`
	Prompt         string  `json:"prompt"`
	Lighting       string  `json:"lighting,omitempty"`        // 光照参数，全剧统一
	ActionContinue string  `json:"action_continue,omitempty"` // 承接上镜动作节点
	Transition     string  `json:"transition,omitempty"`      // 与下镜衔接方式
	SceneLink      string  `json:"scene_link,omitempty"`      // 与上一镜关系: continuous(同场景续接) | transition(转场/换场景)
	Dialogue       *ShotDialogue `json:"dialogue,omitempty"` // 结构化对白，支持多行 lines
	AssetIDs       []int   `json:"asset_ids,omitempty"`
	ImageURL       string  `json:"image_url,omitempty"`
	ImageRemoteURL string  `json:"image_remote_url,omitempty"` // Agnes CDN, e.g. platform-outputs.agnes-ai.space (~24h)
	// Beats is an intra-shot timed action plan. When a shot spans several beats over a
	// longer duration, the model lists what happens at each time node so a single
	// image-to-video generation renders the whole sequence continuously (seamless,
	// no stitching). Empty for short single-beat shots.
	Beats []ShotBeat `json:"beats,omitempty"`
}

// DialogueLine is one spoken line in a shot.
type DialogueLine struct {
	Speaker string `json:"speaker,omitempty"`
	Text    string `json:"text,omitempty"`
}

// ShotDialogue holds one or more dialogue lines for TTS.
// JSON: {"lines":[{"speaker":"石昊","text":"..."}, ...]} or legacy {"speaker","text"}.
type ShotDialogue struct {
	Lines   []DialogueLine `json:"lines,omitempty"`
	Speaker string         `json:"speaker,omitempty"` // legacy single line
	Text    string         `json:"text,omitempty"`
}

func (d ShotDialogue) LinesNormalized() []DialogueLine {
	if len(d.Lines) > 0 {
		out := make([]DialogueLine, 0, len(d.Lines))
		for _, ln := range d.Lines {
			sp := strings.TrimSpace(ln.Speaker)
			tx := strings.TrimSpace(ln.Text)
			if sp == "" && tx == "" {
				continue
			}
			out = append(out, DialogueLine{Speaker: sp, Text: tx})
		}
		if len(out) > 0 {
			return out
		}
	}
	sp := strings.TrimSpace(d.Speaker)
	tx := strings.TrimSpace(d.Text)
	if sp != "" || tx != "" {
		return []DialogueLine{{Speaker: sp, Text: tx}}
	}
	return nil
}

func (d ShotDialogue) IsEmpty() bool {
	return len(d.LinesNormalized()) == 0
}

func (d *ShotDialogue) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || string(data) == "null" {
		*d = ShotDialogue{}
		return nil
	}
	if data[0] == '[' {
		var lines []DialogueLine
		if err := json.Unmarshal(data, &lines); err != nil {
			return err
		}
		*d = ShotDialogue{Lines: lines}
		return nil
	}
	if data[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		*d = parseDialogueString(s)
		return nil
	}
	var aux struct {
		Lines   []DialogueLine `json:"lines"`
		Speaker string         `json:"speaker"`
		Text    string         `json:"text"`
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	*d = ShotDialogue{Lines: aux.Lines, Speaker: aux.Speaker, Text: aux.Text}
	return nil
}

func (d ShotDialogue) MarshalJSON() ([]byte, error) {
	lines := d.LinesNormalized()
	if len(lines) == 0 {
		return []byte("null"), nil
	}
	return json.Marshal(struct {
		Lines []DialogueLine `json:"lines"`
	}{Lines: lines})
}

func parseDialogueString(s string) ShotDialogue {
	s = strings.TrimSpace(s)
	if s == "" {
		return ShotDialogue{}
	}
	if strings.Contains(s, "\n") {
		var lines []DialogueLine
		for _, row := range strings.Split(s, "\n") {
			row = strings.TrimSpace(row)
			if row == "" {
				continue
			}
			if parts := strings.SplitN(row, "|", 2); len(parts) == 2 {
				sp := strings.TrimSpace(parts[0])
				tx := strings.TrimSpace(parts[1])
				if sp != "" && tx != "" {
					lines = append(lines, DialogueLine{Speaker: sp, Text: tx})
				}
				continue
			}
		}
		if len(lines) > 0 {
			return ShotDialogue{Lines: lines}
		}
	}
	if parts := strings.SplitN(s, "|", 2); len(parts) == 2 {
		return ShotDialogue{Lines: []DialogueLine{{Speaker: strings.TrimSpace(parts[0]), Text: strings.TrimSpace(parts[1])}}}
	}
	if parts := strings.SplitN(s, "：", 2); len(parts) == 2 {
		return ShotDialogue{Lines: []DialogueLine{{Speaker: strings.TrimSpace(parts[0]), Text: strings.TrimSpace(parts[1])}}}
	}
	if parts := strings.SplitN(s, ":", 2); len(parts) == 2 && strings.TrimSpace(parts[0]) != "" {
		return ShotDialogue{Lines: []DialogueLine{{Speaker: strings.TrimSpace(parts[0]), Text: strings.TrimSpace(parts[1])}}}
	}
	return ShotDialogue{Lines: []DialogueLine{{Text: s}}}
}

// ShotBeat is one time node inside a shot: at Time seconds (from the shot start),
// Action describes what should be happening on screen. Each beat gets its own
// keyframe still (ImageURL / ImageRemoteURL) which feeds Agnes keyframe video mode.
type ShotBeat struct {
	Time           float64 `json:"time"`
	Action         string  `json:"action"`
	ImagePrompt    string  `json:"image_prompt,omitempty"` // per-beat image prompt generated during storyboarding
	ImageURL       string  `json:"image_url,omitempty"`
	ImageRemoteURL string  `json:"image_remote_url,omitempty"`
}

// SceneLink values describe how a shot connects to the previous shot.
const (
	// SceneLinkContinuous: same scene, seamless continuation — chain the previous
	// shot's last frame into this shot's image-to-video, no transition at the cut.
	SceneLinkContinuous = "continuous"
	// SceneLinkTransition: a new scene / hard cut — render from this shot's own
	// image and apply a visible transition at the boundary.
	SceneLinkTransition = "transition"
)

// IsContinuousLink reports whether this shot continues the previous shot's scene.
func (s StoryboardItem) IsContinuousLink() bool {
	return s.SceneLink == SceneLinkContinuous
}

// ImageArtifact represents a generated image for one shot.
type ImageArtifact struct {
	ShotNumber int    `json:"shot_number"`
	DataURL    string `json:"data_url"`
	LocalPath  string `json:"local_path"`
	Status     string `json:"status"`
}

// NewTask creates a new task with defaults.
func NewTask(id, projectID, script, style string, frameDuration float64, resolution string, fps int, timeout time.Duration) *Task {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	return &Task{
		ID:            id,
		ProjectID:     projectID,
		UserID:        "",
		Script:        script,
		Style:         style,
		FrameDuration: frameDuration,
		Resolution:    resolution,
		FPS:           fps,
		State:         StateWaiting,
		Progress:      0,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
		MaxRetries:    2,
		Timeout:       timeout,
		cancel:        cancel,
		ctx:           ctx,
	}
}

// Context returns the task's context.
func (t *Task) Context() context.Context {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.ctx
}

// Cancel cancels the task context.
func (t *Task) Cancel() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.cancel != nil {
		t.cancel()
	}
}

// SetState transitions the task state.
func (t *Task) SetState(state State, step string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.State = state
	t.Step = step
	t.UpdatedAt = time.Now()
}

// UpdateProgress sets progress (0-100).
func (t *Task) UpdateProgress(p float32) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if p > 100 {
		p = 100
	}
	t.Progress = p
	t.UpdatedAt = time.Now()
}

// SetError marks the task as failed.
func (t *Task) SetError(msg string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.State = StateError
	t.ErrorMessage = msg
	t.Step = "error"
	t.UpdatedAt = time.Now()
}

// CanRetry returns true if retries remain.
func (t *Task) CanRetry() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.RetryCount < t.MaxRetries
}

// IncrementRetry increments retry counter.
func (t *Task) IncrementRetry() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.RetryCount++
}

// Done returns true for terminal states.
func (t *Task) Done() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.State == StateDone || t.State == StateError
}

// IsTimeout returns true if timed out.
func (t *Task) IsTimeout() bool {
	select {
	case <-t.Context().Done():
		return t.Context().Err() == context.DeadlineExceeded
	default:
		return false
	}
}

// Clone returns a copy for serialization.
func (t *Task) Clone() *Task {
	t.mu.RLock()
	defer t.mu.RUnlock()
	cp := *t
	cp.ctx = context.Background()
	cp.cancel = nil
	return &cp
}

func (t *Task) String() string {
	return fmt.Sprintf("Task[%s] state=%s progress=%.1f%% step=%s", t.ID, t.State, t.Progress, t.Step)
}
