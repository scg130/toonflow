package voice

// DefaultNarrationVoice is the fallback Edge TTS voice for narration and unknown speakers.
const DefaultNarrationVoice = "zh-CN-YunyangNeural"

// EdgeVoiceOption is one selectable TTS voice (Microsoft Edge neural).
type EdgeVoiceOption struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Gender string `json:"gender"` // male | female | child
	Style  string `json:"style,omitempty"`
}

// EdgeVoiceCatalog returns zh-CN voices for character assignment UI.
func EdgeVoiceCatalog() []EdgeVoiceOption {
	return []EdgeVoiceOption{
		{ID: "zh-CN-XiaoxiaoNeural", Label: "晓晓 · 温柔女声", Gender: "female", Style: "warm"},
		{ID: "zh-CN-XiaoyiNeural", Label: "晓伊 · 活泼女声", Gender: "female", Style: "lively"},
		{ID: "zh-CN-XiaohanNeural", Label: "晓涵 · 成熟女声", Gender: "female", Style: "calm"},
		{ID: "zh-CN-XiaomengNeural", Label: "晓梦 · 少女", Gender: "female", Style: "youth"},
		{ID: "zh-CN-YunxiNeural", Label: "云希 · 阳光男声", Gender: "male", Style: "youth"},
		{ID: "zh-CN-YunjianNeural", Label: "云健 · 沉稳男声", Gender: "male", Style: "dramatic"},
		{ID: "zh-CN-YunyangNeural", Label: "云扬 · 旁白男声", Gender: "male", Style: "narrator"},
		{ID: "zh-CN-YunxiaNeural", Label: "云夏 · 少年", Gender: "child", Style: "boy"},
	}
}

// DefaultVoiceForGender picks a fallback voice when LLM assignment is missing.
func DefaultVoiceForGender(gender string) string {
	switch gender {
	case "female":
		return "zh-CN-XiaoxiaoNeural"
	case "child", "boy":
		return "zh-CN-YunxiaNeural"
	default:
		return "zh-CN-YunxiNeural"
	}
}

// IsValidEdgeVoice reports whether voiceID is in the catalog.
func IsValidEdgeVoice(voiceID string) bool {
	for _, v := range EdgeVoiceCatalog() {
		if v.ID == voiceID {
			return true
		}
	}
	return voiceID == DefaultNarrationVoice
}
