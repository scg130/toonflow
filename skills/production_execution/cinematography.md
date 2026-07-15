# 分镜七要素与竖屏运镜

短剧分镜必须写全七要素，细节见 `skills/prompts/storyboard_seven.md`。

| 要素 | 字段 | 竖屏短剧默认 |
|------|------|--------------|
| 景别 | shot_size | 中景为主；冲突用特写；交代用全景 |
| 角度 | angle | 平视为主；权力用仰拍；压制用俯拍 |
| 构图 | composition | 脸在 9:16 安全区；三分或中心；少叠脸 |
| 光影 | lighting | 有明确主光方向；脸有明暗；忌空写“氛围” |
| 色调 | color_tone | 高对比；同场色调延续；换场再改冷暖 |
| 动势 | motion | **只写一条**主运动（推/拉/摇/移/手持/定） |
| 转场 | transition | 同场 soft dissolve；换场 fade black / wipe |

## 图生图 / 图生视频
- 静帧 prompt 可带构图/光影/色调。
- 视频 prompt 优先吃动势 + 景别变化；禁止 emotional / cinematic 空词。
- `camera` = 景别 + 角度 + 动势。
