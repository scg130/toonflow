package camera

import "strings"

// MapCameraToVideoMotion maps storyboard camera notes to English motion prompt fragments.
func MapCameraToVideoMotion(camera string) string {
	c := strings.TrimSpace(camera)
	if c == "" {
		return "cinematic camera with subtle motivated movement"
	}
	lower := strings.ToLower(c)
	switch {
	case strings.Contains(lower, "dolly zoom") || strings.Contains(lower, "希区库克") || strings.Contains(lower, "vertigo"):
		return "dolly zoom vertigo effect, background compression while subject scales"
	case strings.Contains(lower, "rack focus") || strings.Contains(lower, "跟焦") || strings.Contains(lower, "移焦"):
		return "rack focus pull between foreground and background planes"
	case strings.Contains(lower, "slow motion") || strings.Contains(lower, "慢镜头") || strings.Contains(lower, "升格"):
		return "slow motion high frame rate capture, smooth temporal detail"
	case strings.Contains(lower, "推近") || strings.Contains(lower, "push") || strings.Contains(lower, "dolly in"):
		return "slow cinematic dolly push-in toward subject"
	case strings.Contains(lower, "拉远") || strings.Contains(lower, "pull") || strings.Contains(lower, "dolly out"):
		return "smooth dolly pull-back revealing environment"
	case strings.Contains(lower, "环绕") || strings.Contains(lower, "orbit"):
		return "orbital camera circling around subject"
	case strings.Contains(lower, "仰拍") || strings.Contains(lower, "low angle"):
		return "low angle heroic camera looking up at subject"
	case strings.Contains(lower, "俯拍") || strings.Contains(lower, "high angle"):
		return "high angle overhead establishing shot"
	case strings.Contains(lower, "跟拍") || strings.Contains(lower, "tracking"):
		return "tracking shot following subject movement"
	case strings.Contains(lower, "摇") || strings.Contains(lower, "pan"):
		return "smooth horizontal pan camera movement"
	case strings.Contains(lower, "tilt") || strings.Contains(lower, "俯仰"):
		return "cinematic tilt camera movement"
	case strings.Contains(lower, "crane") || strings.Contains(lower, "升降"):
		return "crane shot vertical camera movement"
	case strings.Contains(lower, "固定") || strings.Contains(lower, "静止") || strings.Contains(lower, "static"):
		return "locked-off tripod shot with living scene motion"
	case strings.Contains(lower, "手持") || strings.Contains(lower, "handheld"):
		return "subtle handheld documentary camera energy"
	case strings.Contains(lower, "航拍") || strings.Contains(lower, "drone"):
		return "aerial drone flyover cinematic movement"
	default:
		return "camera motion: " + c
	}
}
