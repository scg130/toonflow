package storyboard

import (
	"os"
	"strings"
	"testing"

	"toonflow/task"
)

func TestNormalizeStoryboardItems_sceneLink(t *testing.T) {
	items := []task.StoryboardItem{
		{ShotNumber: 1, Scene: "村口", Description: "开场", Prompt: "p"},                         // first → transition
		{ShotNumber: 2, Scene: "村口", Description: "顺接", Prompt: "p"},                         // same scene, empty → continuous
		{ShotNumber: 3, Scene: "战场", Description: "换场", Prompt: "p"},                         // scene changed → transition
		{ShotNumber: 4, Scene: "战场", Description: "闪回", Prompt: "p", SceneLink: "transition"}, // same scene but model forces transition (flashback)
		{ShotNumber: 5, Scene: "战场", Description: "续", Prompt: "p", SceneLink: "续接"},          // model says continuous (zh)
	}
	out := NormalizeStoryboardItems(items)
	want := []string{
		task.SceneLinkTransition,
		task.SceneLinkContinuous,
		task.SceneLinkTransition,
		task.SceneLinkTransition,
		task.SceneLinkContinuous,
	}
	if len(out) != len(want) {
		t.Fatalf("expected %d items, got %d", len(want), len(out))
	}
	for i, w := range want {
		if out[i].SceneLink != w {
			t.Fatalf("shot %d scene_link = %q, want %q", out[i].ShotNumber, out[i].SceneLink, w)
		}
	}
}

func TestParseStoryboardResponse_objectWrapper(t *testing.T) {
	// JSON-mode returns an object wrapper.
	obj := `{"shots":[{"shot_number":1,"scene":"虚空","description":"焦黑树桩","duration":4,"dialogue":"石昊：走了","prompt":"wide"},{"shot_number":2,"scene":"虚空","description":"石昊起身","duration":3,"prompt":"close"}]}`
	items, err := ParseStoryboardResponse(obj)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 shots from wrapper, got %d", len(items))
	}
	if items[1].Dialogue != "" || items[0].Dialogue != "石昊：走了" {
		t.Fatalf("dialogue field not carried from wrapper: %+v", items)
	}

	// Bare array must still parse (fallback path).
	arr := `[{"shot_number":1,"scene":"s","description":"d","duration":3,"prompt":"p"}]`
	items2, err := ParseStoryboardResponse(arr)
	if err != nil || len(items2) != 1 {
		t.Fatalf("bare array fallback failed: %d %v", len(items2), err)
	}
}

const sampleTableStoryboard = `
#### **【第一幕：最后的温柔】 (00:00 - 00:12)**

| 镜头号 | 景别 | 画面描述 (Visual) | 运镜 (Camera) | 音效/台词 (Audio) | 时长 |
| :--- | :--- | :--- | :--- | :--- | :--- |
| **VC01** | **特写 (ECU)** | **柳叶脉络**：微观视角。一片巨大的柳叶。 | 极慢推镜头 | 音效：静谧 | 3s |
| **VC02** | **近景 (CU)** | **石昊泪眼**：石昊跪在地上。 | 轻微手持晃动 | 石昊："柳神..." | 4s |

#### **【第二幕：黑暗降临】 (00:12 - 00:28)**

| 镜头号 | 景别 | 画面描述 (Visual) | 运镜 (Camera) | 音效/台词 (Audio) | 时长 |
| :--- | :--- | :--- | :--- | :--- | :--- |
| **VC03** | **中景 (MS)** | **柳神虚影**：柳神半身虚影。 | 仰拍环绕 | 柳神："小石头..." | 5s |
`

func TestParseTableStoryboard(t *testing.T) {
	items, err := ParseStoryboardResponse(sampleTableStoryboard)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) < 3 {
		t.Fatalf("expected >= 3 shots, got %d", len(items))
	}
	if items[0].ShotNumber != 1 {
		t.Fatalf("expected shot 1, got %d", items[0].ShotNumber)
	}
	if !strings.Contains(items[0].Description, "柳叶") {
		t.Fatalf("unexpected description: %s", items[0].Description)
	}
	if items[0].Scene == "" {
		t.Fatal("expected scene from act header")
	}
	if items[2].ShotNumber != 3 {
		t.Fatalf("expected shot 3, got %d", items[2].ShotNumber)
	}
	if items[1].Dialogue == "" || !strings.Contains(items[1].Dialogue, "石昊") {
		t.Fatalf("expected dialogue on shot 2, got %q", items[1].Dialogue)
	}
}

func TestStoryboardScorePenalizesFallback(t *testing.T) {
	bad := []task.StoryboardItem{{Description: "Here is the professional storyboard breakdown for EP01 | Shot # | Scene |"}}
	good := []task.StoryboardItem{{Description: "a"}, {Description: "b"}, {Description: "c"}}
	if StoryboardScore(bad) >= StoryboardScore(good) {
		t.Fatal("fallback should score lower than real shots")
	}
}

func TestStoryboardScorePenalizesSingleShot(t *testing.T) {
	single := []task.StoryboardItem{{Description: "one shot only"}}
	multi := []task.StoryboardItem{{Description: "a"}, {Description: "b"}}
	if StoryboardScore(single) >= StoryboardScore(multi) {
		t.Fatal("single shot should score lower than multi")
	}
}

func TestStoryboardScorePrefersDenseLongShots(t *testing.T) {
	thin := make([]task.StoryboardItem, 12)
	for i := range thin {
		thin[i] = task.StoryboardItem{Description: "x", Duration: 10, Beats: []task.ShotBeat{{Time: 0, Action: "a"}, {Time: 5, Action: "b"}}}
	}
	dense := []task.StoryboardItem{
		{Description: "arc1", Duration: 15, Beats: make([]task.ShotBeat, 5)},
		{Description: "arc2", Duration: 16, Beats: make([]task.ShotBeat, 6)},
		{Description: "arc3", Duration: 14, Beats: make([]task.ShotBeat, 5)},
		{Description: "arc4", Duration: 15, Beats: make([]task.ShotBeat, 5)},
	}
	for i := range dense {
		for j := range dense[i].Beats {
			dense[i].Beats[j] = task.ShotBeat{Time: float64(j), Action: "beat"}
		}
	}
	if StoryboardScore(dense) <= StoryboardScore(thin) {
		t.Fatalf("dense long shots should score higher than many thin shots: dense=%d thin=%d", StoryboardScore(dense), StoryboardScore(thin))
	}
}

func TestEnsureShotBeatsDensifies(t *testing.T) {
	out := NormalizeStoryboardItems([]task.StoryboardItem{{
		ShotNumber: 1, Description: "石昊挥拳", Duration: 15,
		Beats: []task.ShotBeat{{Time: 0, Action: "起手"}, {Time: 7, Action: "命中"}},
	}})
	if len(out) != 1 {
		t.Fatalf("expected 1 item")
	}
	if len(out[0].Beats) < 5 {
		t.Fatalf("15s shot should densify to >=5 beats, got %d", len(out[0].Beats))
	}
}

func TestMinShotsForScript(t *testing.T) {
	short := MinShotsForScript("简短剧本")
	if short < 3 {
		t.Fatalf("expected min 3 for short script, got %d", short)
	}
	long := strings.Repeat("这是一段较长的剧本内容，包含对白和动作描述。", 100)
	got := MinShotsForScript(long)
	if got < 3 || got > 12 {
		t.Fatalf("long-shot min should stay in 3–12 for dense strategy, got %d", got)
	}
	// Old /180 rule would have asked for many more; denser long-shot rule must be leaner.
	if got >= len([]rune(long))/180 {
		t.Fatalf("min shots too high for long-shot strategy: got %d", got)
	}
	scenes := "【第一场 柳树下】\n对白\n【第二场 战场】\n动作\n【第三场 回忆】\n结尾"
	if MinShotsForScript(scenes) < 3 {
		t.Fatalf("expected at least scene count")
	}
}

func TestIsAdequateStoryboard(t *testing.T) {
	if IsAdequateStoryboard([]task.StoryboardItem{{Description: "x"}}, 4) {
		t.Fatal("single shot should not be adequate when min is 4")
	}
	items := make([]task.StoryboardItem, 5)
	for i := range items {
		items[i] = task.StoryboardItem{Description: "shot"}
	}
	if !IsAdequateStoryboard(items, 4) {
		t.Fatal("5 shots should be adequate for min 4")
	}
}

func TestParseRealVCScript(t *testing.T) {
	data, err := os.ReadFile("/tmp/sb.txt")
	if err != nil {
		t.Skip("no real sample at /tmp/sb.txt")
	}
	items, err := ParseStoryboardResponse(string(data))
	if err != nil {
		t.Fatal(err)
	}
	if len(items) < 10 {
		t.Fatalf("expected 10+ shots from real chat, got %d", len(items))
	}
}
