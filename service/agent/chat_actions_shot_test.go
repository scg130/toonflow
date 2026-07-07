package agent

import "testing"

func TestInferShotNumberFromUserMessage(t *testing.T) {
	cases := []struct {
		msg  string
		want int
		ok   bool
	}{
		{"为第二镜生成图片", 2, true},
		{"让第2镜生成图片", 2, true},
		{"第 3 镜 出图", 3, true},
		{"镜5生成", 5, true},
		{"生成图片", 0, false},
	}
	for _, tc := range cases {
		got, ok := inferShotNumberFromUserMessage(tc.msg)
		if ok != tc.ok || got != tc.want {
			t.Fatalf("inferShotNumberFromUserMessage(%q) = (%d, %v), want (%d, %v)", tc.msg, got, ok, tc.want, tc.ok)
		}
	}
}

func TestEnrichIntentFromUserMessage(t *testing.T) {
	intent := &ChatActionIntent{Type: "generate_shot_image"}
	EnrichIntentFromUserMessage(intent, "为第二镜生成图片")
	if intent.Params["shot_number"] != "2" {
		t.Fatalf("shot_number = %q, want 2", intent.Params["shot_number"])
	}
}
