package asset

import "testing"

func TestExtractSpeakerNamesFromScript(t *testing.T) {
	script := `第一场
石昊：柳神……你怎么会变成这样？
柳神|天地将倾，你要守住这一界。
【禹帝】混沌未开。`
	got := ExtractSpeakerNamesFromScript(script)
	want := []string{"石昊", "柳神", "禹帝"}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
}

func TestAssignChineseRoleNames(t *testing.T) {
	script := `石昊：台词一
柳神：台词二`
	items := []extractAssetItem{
		{Name: "ShiHao", Type: "role", CharacterID: "shi_hao"},
		{Name: "LiuShen", Type: "role", CharacterID: "liu_shen"},
	}
	AssignChineseRoleNames(items, script)
	if items[0].Name != "石昊" || items[1].Name != "柳神" {
		t.Fatalf("names=%q %q", items[0].Name, items[1].Name)
	}
}
