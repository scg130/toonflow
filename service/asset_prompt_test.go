package service

import (
	"strings"
	"testing"

	"toonflow/task"
)

const shiHaoAssetDesc = `8K 超清，国漫 3D CG 动画渲染，《完美世界》官方建模质感，全身立绘，荒天帝石昊，冷峻绝世俊美青年，墨黑长发狂束高马尾，金色帝印额饰，狭长鎏金竖金瞳，孤傲睥睨神情，一手轻握上古兽首鎏金面具，玄黑红金边天帝长袍，衣身流转金色天道符文，鎏金神兽浮雕肩甲，繁复云纹刺绣布料，身后漫天红枫云海，细碎霞光，柔和体积光，细腻发丝布料纹理，完整全身构图，低角度氛围感镜头，史诗仙侠氛围，皮肤通透细腻，金属高光质感，极致细节，电影级光影，masterpiece，顶级原画
masterpiece, best quality, 8K, ultra detailed, Chinese 3D CG anime, Perfect World official art style, full body portrait, Huang Di Shi Hao, handsome cold aloof young man, long black hair tied high ponytail, golden emperor mark on forehead, narrow golden vertical pupils, arrogant gaze, one hand holding ancient beast golden mask, black emperor robe with red and gold trim, flowing golden dao runes on clothes, golden beast carved shoulder armor, intricate cloud embroidery, red maple sea background, soft glow, volumetric light, delicate hair and fabric texture, full body shot, low angle cinematic shot, epic xianxia atmosphere, realistic delicate skin, metallic highlight, cinematic lighting`

func TestRoleAssetDescForShot_stripsMaskUnlessMentioned(t *testing.T) {
	plain := task.StoryboardItem{
		ShotNumber:  1,
		Description: "石昊猛然起身，赤红双目",
		Prompt:      "wide shot, character_id: ShiHao",
	}
	stripped := RoleAssetDescForShot(shiHaoAssetDesc, plain)
	lower := strings.ToLower(stripped)
	if strings.Contains(lower, "mask") || strings.Contains(stripped, "面具") {
		t.Fatalf("expected mask stripped from default shot, got: %s", stripped)
	}
	if !strings.Contains(stripped, "石昊") && !strings.Contains(lower, "shi hao") {
		t.Fatalf("expected identity kept, got: %s", stripped)
	}

	withMask := task.StoryboardItem{
		Description: "石昊举起上古兽首面具",
		Prompt:      "close-up holding golden mask",
	}
	full := RoleAssetDescForShot(shiHaoAssetDesc, withMask)
	if !strings.Contains(strings.ToLower(full), "mask") && !strings.Contains(full, "面具") {
		t.Fatalf("expected mask kept when shot mentions it, got: %s", full)
	}
}

func TestRoleReferenceImageURL(t *testing.T) {
	if roleReferenceImageURL(ProjectAsset{Type: "role", ParentID: 0, FileURL: "https://cdn/x.png"}) != true {
		t.Fatal("parent role should be reference")
	}
	if roleReferenceImageURL(ProjectAsset{Type: "prop", FileURL: "https://cdn/mask.png"}) {
		t.Fatal("prop should not be reference")
	}
	if roleReferenceImageURL(ProjectAsset{Type: "role", ParentID: 1, FileURL: "https://cdn/side.png"}) {
		t.Fatal("child turnaround should not be reference")
	}
}
