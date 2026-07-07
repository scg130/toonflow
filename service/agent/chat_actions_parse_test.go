package agent

import "testing"

func TestParseActionFromReply(t *testing.T) {
	cases := []struct {
		name       string
		in         string
		wantType   string
		wantShot   string
		wantIntent bool
	}{
		{
			name:       "own line action",
			in:         "好的，正在生成剧本\nACTION:generate_script",
			wantType:   "generate_script",
			wantIntent: true,
		},
		{
			name:       "full width colon",
			in:         "好的\nACTION：generate_storyboard",
			wantType:   "generate_storyboard",
			wantIntent: true,
		},
		{
			name:       "code fenced action",
			in:         "好的\n`ACTION:extract_assets`",
			wantType:   "extract_assets",
			wantIntent: true,
		},
		{
			name:       "markdown bold action",
			in:         "好的\n**ACTION:generate_skeleton**",
			wantType:   "generate_skeleton",
			wantIntent: true,
		},
		{
			name:       "shot on preceding line",
			in:         "好的，正在为第 2 镜生成图片\nSHOT:2\nACTION:generate_shot_image",
			wantType:   "generate_shot_image",
			wantShot:   "2",
			wantIntent: true,
		},
		{
			name:       "inline shot token",
			in:         "好的\nACTION:generate_shot_image:3",
			wantType:   "generate_shot_image",
			wantShot:   "3",
			wantIntent: true,
		},
		{
			name:       "non-whitelisted dropped",
			in:         "好的\nACTION:delete_everything",
			wantIntent: false,
		},
		{
			name:       "prose mention not triggered",
			in:         "你可以用 ACTION:generate_script 来生成剧本",
			wantIntent: false,
		},
		{
			name:       "pure chat no action",
			in:         "这个剧本的节奏还不错，可以考虑加快前半段。",
			wantIntent: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, intent := parseActionFromReply(tc.in)
			if tc.wantIntent != (intent != nil) {
				t.Fatalf("intent presence = %v, want %v (in=%q)", intent != nil, tc.wantIntent, tc.in)
			}
			if !tc.wantIntent {
				return
			}
			if intent.Type != tc.wantType {
				t.Fatalf("type = %q, want %q", intent.Type, tc.wantType)
			}
			if tc.wantShot != "" && intent.Params["shot_number"] != tc.wantShot {
				t.Fatalf("shot_number = %q, want %q", intent.Params["shot_number"], tc.wantShot)
			}
		})
	}
}
