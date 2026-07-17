package storyboard

import (
	"testing"

	"toonflow/task"
)

func TestMergeStoryboardMedia_preservesBeatImages(t *testing.T) {
	existing := []task.StoryboardItem{{
		ShotNumber: 1,
		ImageURL:   "/output/old/shot_001.png",
		Beats: []task.ShotBeat{
			{Time: 0, Action: "old a0", ImageURL: "/output/t/k0.png", ImageRemoteURL: "https://cdn.example/k0.png"},
			{Time: 4, Action: "old a1", ImageURL: "/output/t/k1.png", ImageRemoteURL: "https://cdn.example/k1.png"},
		},
	}}
	incoming := []task.StoryboardItem{{
		ShotNumber:  1,
		Description: "refreshed text",
		Beats: []task.ShotBeat{
			{Time: 0, Action: "new a0", ImagePrompt: "new prompt 0"},
			{Time: 4, Action: "new a1", ImagePrompt: "new prompt 1"},
		},
	}}
	got := MergeStoryboardMedia(existing, incoming)
	if len(got) != 1 || len(got[0].Beats) != 2 {
		t.Fatalf("unexpected merge result: %#v", got)
	}
	if got[0].Beats[0].ImageURL != "/output/t/k0.png" || got[0].Beats[1].ImageRemoteURL != "https://cdn.example/k1.png" {
		t.Fatalf("beat media not preserved: %#v", got[0].Beats)
	}
	if got[0].ImageURL != "/output/t/k0.png" {
		t.Fatalf("shot-level url should follow first beat: %q", got[0].ImageURL)
	}
	if got[0].Beats[0].Action != "new a0" {
		t.Fatalf("incoming action should win: %q", got[0].Beats[0].Action)
	}
}
