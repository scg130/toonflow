package camera

import "strings"

// motionRule is one storyboard-camera → I2V motion mapping.
// More specific phrases must appear before generic ones (e.g. 慢推 before 推近).
type motionRule struct {
	needles []string
	out     string
}

// lexicon inspired by AI漫剧分镜手册 (运镜/近景物理路径), stripped of ARRI / 电影感 / 8K slop.
var videoMotionRules = []motionRule{
	{[]string{"dolly zoom", "希区库克", "希区柯克", "vertigo"},
		"dolly zoom: lens pulls while camera pushes, face stays size, background warps"},
	{[]string{"rack focus", "跟焦", "移焦"},
		"fast rack focus snap from eyes to held object then back"},
	{[]string{"slow motion", "慢镜头", "升格"},
		"impact slow-motion for one beat then resume normal speed on face"},

	// —— 运镜与速度（具名动作）——
	{[]string{"横移揭示", "横移", "侧移揭示", "lateral reveal"},
		"horizontal dolly past wall or pillar, gradually reveal second subject or ambush"},
	{[]string{"慢推压迫", "慢推", "压迫推"},
		"slow continuous push-in that tightens the frame and pressure on the face"},
	{[]string{"骤停定格", "骤停", "定格", "freeze mid"},
		"fast push then hard freeze for one beat on contact point or weapon tip"},
	{[]string{"手持奔逃", "奔跑跟拍", "奔逃"},
		"handheld running follow, natural shake, shoulders of passers blur past lens"},
	{[]string{"长廊后退", "倒退跟拍", "tracking backward", "track back"},
		"smooth track backward while subject advances toward camera along depth lines"},
	{[]string{"低空掠行", "贴地跟", "贴地冲"},
		"low near-ground rush forward, dust parting at sides, feet keep pace with lens"},
	{[]string{"空中跟旋", "跟旋", "旋转眩晕", "眩晕旋转"},
		"tight spin around tumbling or fallen subject for one dizzy beat then settle"},
	{[]string{"斜向穿林", "穿林"},
		"angled glide through trees, leaves brush past lens, dappled light flickers"},
	{[]string{"跟随箭矢", "追箭", "追随箭"},
		"camera locks onto flying projectile through rain or mist until impact"},
	{[]string{"门缝推进", "门缝推"},
		"slow push through door crack into interior, deepen into room"},
	{[]string{"顶视包围", "顶视", "鸟瞰围"},
		"top-down framing, subjects held inside a circle of surrounding space"},
	{[]string{"战场俯扫", "俯扫"},
		"high-angle sweeping pan across the action plane, keep readable subject"},
	{[]string{"高空俯瞰", "航拍俯"},
		"high aerial descend into subject then settle to readable framing"},
	{[]string{"速度变换", "变速运镜"},
		"speed ramp: surge forward then brief settle on face"},

	// —— 景别 / 角度 / 通用动势 ——
	{[]string{"极特写", "眼部", "extreme close"},
		"extreme close-up: eyes and mouth fill frame, lids and lips move"},
	{[]string{"特写", "close-up", "close up"},
		"tight close-up, face fills vertical frame, brows lids lips move"},
	{[]string{"近景", "medium close"},
		"medium close-up chest-up, head and shoulders move, vertical 9:16"},
	{[]string{"推近", "push", "dolly in", "推镜"},
		"fast dolly push-in into face until cheeks fill sides"},
	{[]string{"拉远", "pull", "dolly out", "拉镜"},
		"quick dolly pull-back to reveal full body and room"},
	{[]string{"环绕", "orbit"},
		"tight orbit around subject, keep face centered in 9:16"},
	{[]string{"仰拍", "low angle"},
		"low angle looking up under chin, subject towers in frame"},
	{[]string{"俯拍", "high angle"},
		"high angle looking down on shoulders and crown"},
	{[]string{"跟拍", "跟移", "tracking", "跟随"},
		"urgent tracking follow, keep subject centered in 9:16"},
	{[]string{"摇", "pan"},
		"snappy pan to second face reaction, stop hard"},
	{[]string{"tilt", "俯仰"},
		"tilt up from hands to face, or tilt down from face to hands"},
	{[]string{"crane", "升降"},
		"swift crane rise into low-angle full body then settle"},
	{[]string{"固定", "静止", "定镜", "static"},
		"locked tripod frame; only subject limbs and particles move"},
	{[]string{"手持", "handheld"},
		"handheld micro-shake increases as subject steps forward"},
	{[]string{"航拍", "drone"},
		"fast aerial descend into subject then cut energy to close framing"},
	{[]string{"荷兰", "dutch"},
		"dutch angle tilted horizon, subject leans against tilt"},
}

// MapCameraToVideoMotion maps storyboard camera notes to punchy vertical short-drama motion.
// Prefer concrete camera path/framing — never opaque "emotion" words the I2V model cannot act on.
func MapCameraToVideoMotion(camera string) string {
	c := strings.TrimSpace(camera)
	if c == "" {
		return "fast vertical push-in until face fills frame"
	}
	lower := strings.ToLower(c)
	for _, rule := range videoMotionRules {
		for _, n := range rule.needles {
			if strings.Contains(lower, strings.ToLower(n)) {
				return rule.out
			}
		}
	}
	return "vertical short-drama camera: " + c
}

// MicroExpressionMotion returns a concrete face/hand micro-action line when beat text
// matches common 近景表情分镜 hints (泪、瞳孔、拳心、回头…). Empty if none.
func MicroExpressionMotion(blob string) string {
	b := strings.ToLower(strings.TrimSpace(blob))
	if b == "" {
		return ""
	}
	rules := []motionRule{
		{[]string{"瞳孔收缩", "瞳孔骤缩", "pupil"}, "pupils tighten sharply; lids freeze half a beat"},
		{[]string{"泪痕", "泪光", "含泪", "泪落", "tears"}, "tear line catches light; lids tremble then hold"},
		{[]string{"肩膀颤抖", "肩颤"}, "shoulders tremble in small pulses, breath hitch"},
		{[]string{"拳心收紧", "握拳", "拳握紧", "clench"}, "fist clenches tighter, knuckles whiten, forearm tense"},
		{[]string{"回头一瞬", "猛回头", "glance back"}, "head snaps back for one glance then returns"},
		{[]string{"眼神错开", "目光躲开", "移开视线"}, "eyes break contact sideways, jaw stays set"},
		{[]string{"笑意破碎", "苦笑", "笑意崩"}, "smile collapses from eyes first then mouth"},
		{[]string{"额汗", "冷汗", "sweat"}, "bead of sweat slides from temple along cheek"},
		{[]string{"发丝遮眼", "发丝遮"}, "loose hair strand drifts across eye, then brushed aside"},
		{[]string{"指尖停顿", "手指停"}, "fingertips freeze mid-reach for one beat then continue"},
		{[]string{"鼻尖相近", "鼻尖"}, "faces ease closer until noses nearly touch, breath visible"},
		{[]string{"侧脸逆光", "逆光侧脸"}, "profile turns into rim light; cheek edge stays sharp"},
	}
	for _, rule := range rules {
		for _, n := range rule.needles {
			if strings.Contains(b, strings.ToLower(n)) {
				return rule.out
			}
		}
	}
	return ""
}
