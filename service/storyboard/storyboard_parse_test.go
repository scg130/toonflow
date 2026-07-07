package storyboard

import (
	"os"
	"strings"
	"testing"

	"toonflow/task"
)

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

func TestMinShotsForScript(t *testing.T) {
	short := MinShotsForScript("简短剧本")
	if short < 4 {
		t.Fatalf("expected min 4 for short script, got %d", short)
	}
	long := strings.Repeat("这是一段较长的剧本内容，包含对白和动作描述。", 100)
	if MinShotsForScript(long) < 8 {
		t.Fatalf("expected more shots for long script, got %d", MinShotsForScript(long))
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
