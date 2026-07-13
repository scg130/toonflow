package camera

import "strings"

// MapCameraToVideoMotion maps storyboard camera notes to punchy vertical short-drama motion.
// Prefer 红果/抖音短剧语感：强推近、特写、手持冲击，避免「slow cinematic subtle」电影散文运镜。
func MapCameraToVideoMotion(camera string) string {
	c := strings.TrimSpace(camera)
	if c == "" {
		return "aggressive vertical short-drama push-in on face, emotional close-up"
	}
	lower := strings.ToLower(c)
	switch {
	case strings.Contains(lower, "dolly zoom") || strings.Contains(lower, "希区库克") || strings.Contains(lower, "vertigo"):
		return "dramatic dolly zoom vertigo hit, background warps while face fills frame"
	case strings.Contains(lower, "rack focus") || strings.Contains(lower, "跟焦") || strings.Contains(lower, "移焦"):
		return "fast rack focus snap between eyes and object, short-drama tension"
	case strings.Contains(lower, "slow motion") || strings.Contains(lower, "慢镜头") || strings.Contains(lower, "升格"):
		return "impact slow-motion burst then resume, peak emotion freeze on face"
	case strings.Contains(lower, "极特写") || strings.Contains(lower, "眼部") || strings.Contains(lower, "extreme close"):
		return "extreme close-up on eyes/mouth, micro-expression readable, punchy framing"
	case strings.Contains(lower, "特写") || strings.Contains(lower, "close-up") || strings.Contains(lower, "close up"):
		return "tight emotional close-up, face fills vertical frame, short-drama intensity"
	case strings.Contains(lower, "近景") || strings.Contains(lower, "medium close"):
		return "medium close-up chest-up, strong facial acting, vertical short-drama framing"
	case strings.Contains(lower, "推近") || strings.Contains(lower, "push") || strings.Contains(lower, "dolly in"):
		return "fast aggressive dolly push-in into face, rising emotion"
	case strings.Contains(lower, "拉远") || strings.Contains(lower, "pull") || strings.Contains(lower, "dolly out"):
		return "quick pull-back reveal after impact beat"
	case strings.Contains(lower, "环绕") || strings.Contains(lower, "orbit"):
		return "tight orbit around subject, energy rising, short-drama hero shot"
	case strings.Contains(lower, "仰拍") || strings.Contains(lower, "low angle"):
		return "low angle power shot looking up, vertical dominance"
	case strings.Contains(lower, "俯拍") || strings.Contains(lower, "high angle"):
		return "high angle vulnerable shot looking down on subject"
	case strings.Contains(lower, "跟拍") || strings.Contains(lower, "tracking"):
		return "urgent tracking follow, keep subject centered in 9:16"
	case strings.Contains(lower, "摇") || strings.Contains(lower, "pan"):
		return "snappy pan to reveal reaction, short-drama cut energy"
	case strings.Contains(lower, "tilt") || strings.Contains(lower, "俯仰"):
		return "dramatic tilt up to face or down to hands"
	case strings.Contains(lower, "crane") || strings.Contains(lower, "升降"):
		return "swift crane rise into hero low-angle, short-drama climax"
	case strings.Contains(lower, "固定") || strings.Contains(lower, "静止") || strings.Contains(lower, "static"):
		return "locked frame with intense subject micro-motion and particle motion"
	case strings.Contains(lower, "手持") || strings.Contains(lower, "handheld"):
		return "handheld shake intensifying with emotion, documentary urgency"
	case strings.Contains(lower, "航拍") || strings.Contains(lower, "drone"):
		return "fast aerial descend into subject, then punch to close-up energy"
	case strings.Contains(lower, "荷兰") || strings.Contains(lower, "dutch"):
		return "dutch angle tension, unstable short-drama conflict frame"
	default:
		return "vertical short-drama camera: " + c
	}
}
