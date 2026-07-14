package camera

import "strings"

// MapCameraToVideoMotion maps storyboard camera notes to punchy vertical short-drama motion.
// Prefer concrete camera path/framing — never opaque "emotion" words the I2V model cannot act on.
func MapCameraToVideoMotion(camera string) string {
	c := strings.TrimSpace(camera)
	if c == "" {
		return "fast vertical push-in until face fills frame"
	}
	lower := strings.ToLower(c)
	switch {
	case strings.Contains(lower, "dolly zoom") || strings.Contains(lower, "希区库克") || strings.Contains(lower, "vertigo"):
		return "dolly zoom: lens pulls while camera pushes, face stays size, background warps"
	case strings.Contains(lower, "rack focus") || strings.Contains(lower, "跟焦") || strings.Contains(lower, "移焦"):
		return "fast rack focus snap from eyes to held object then back"
	case strings.Contains(lower, "slow motion") || strings.Contains(lower, "慢镜头") || strings.Contains(lower, "升格"):
		return "impact slow-motion for one beat then resume normal speed on face"
	case strings.Contains(lower, "极特写") || strings.Contains(lower, "眼部") || strings.Contains(lower, "extreme close"):
		return "extreme close-up: eyes and mouth fill frame, lids and lips move"
	case strings.Contains(lower, "特写") || strings.Contains(lower, "close-up") || strings.Contains(lower, "close up"):
		return "tight close-up, face fills vertical frame, brows lids lips move"
	case strings.Contains(lower, "近景") || strings.Contains(lower, "medium close"):
		return "medium close-up chest-up, head and shoulders move, vertical 9:16"
	case strings.Contains(lower, "推近") || strings.Contains(lower, "push") || strings.Contains(lower, "dolly in"):
		return "fast dolly push-in into face until cheeks fill sides"
	case strings.Contains(lower, "拉远") || strings.Contains(lower, "pull") || strings.Contains(lower, "dolly out"):
		return "quick dolly pull-back to reveal full body and room"
	case strings.Contains(lower, "环绕") || strings.Contains(lower, "orbit"):
		return "tight orbit around subject, keep face centered in 9:16"
	case strings.Contains(lower, "仰拍") || strings.Contains(lower, "low angle"):
		return "low angle looking up under chin, subject towers in frame"
	case strings.Contains(lower, "俯拍") || strings.Contains(lower, "high angle"):
		return "high angle looking down on shoulders and crown"
	case strings.Contains(lower, "跟拍") || strings.Contains(lower, "tracking"):
		return "urgent tracking follow, keep subject centered in 9:16"
	case strings.Contains(lower, "摇") || strings.Contains(lower, "pan"):
		return "snappy pan to second face reaction, stop hard"
	case strings.Contains(lower, "tilt") || strings.Contains(lower, "俯仰"):
		return "tilt up from hands to face, or tilt down from face to hands"
	case strings.Contains(lower, "crane") || strings.Contains(lower, "升降"):
		return "swift crane rise into low-angle full body then settle"
	case strings.Contains(lower, "固定") || strings.Contains(lower, "静止") || strings.Contains(lower, "static"):
		return "locked tripod frame; only subject limbs and particles move"
	case strings.Contains(lower, "手持") || strings.Contains(lower, "handheld"):
		return "handheld micro-shake increases as subject steps forward"
	case strings.Contains(lower, "航拍") || strings.Contains(lower, "drone"):
		return "fast aerial descend into subject then cut energy to close framing"
	case strings.Contains(lower, "荷兰") || strings.Contains(lower, "dutch"):
		return "dutch angle tilted horizon, subject leans against tilt"
	default:
		return "vertical short-drama camera: " + c
	}
}
