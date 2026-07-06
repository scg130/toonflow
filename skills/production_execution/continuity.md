# 全链路连贯性强制规则（文本→资产→时序→成片）

## 文本层（剧本 / 分镜）

- 每镜必须输出 `lighting`（光照参数）、`action_continue`（承接上镜动作）、`transition`（与下镜衔接方式）
- 相邻镜头运镜方向延续，禁止前镜环绕后镜突然拉远
- 同场景角色 `character_id` 与服饰特征全文不变

## 资产层（生图）

- 全剧复用项目 `style_anchor`，禁止单镜随机画风偏移
- 重绘仅允许动作微调，角色五官/服饰/场景光影不可变更

## 时序层（I2V）

- Prompt 须含：`temporal encoding, keyframe interpolation, feature anchoring, frame-to-frame continuity`
- 负向须含：`flickering, jitter, morphing, warping, stuttering`
- 批量视频按镜号串行，上一镜末帧作为下一镜连贯参考

## 成片层（剪辑 / 质检）

- 14 维连贯性打分，总分低于 60 的片段自动淘汰
- 多版本生成时保留最高分版本
- 导出统一调色、饱和度、转场节奏
