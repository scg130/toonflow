# 全链路连贯性强制规则（文本→资产→时序→成片）

## AI 分镜写作原则（剧本须预埋）

- 每个镜头写清**目标事件**，不只写情绪（❌「悲愤欲绝」→ ✅「树桩化灰、手指僵住」）
- 结构：**画面 + 动作 + 反应 + 台词**，台词绑定嘴部动作时机
- action_continue 链：上镜末状态 → 本镜起始 → 本镜末状态
- 禁止抽象特效词：空间崩碎、杀念化焰、威压撕云 → 改为可见变化（碎石悬浮、红光渐强、灰烬飘散）

## 文本层（剧本 / 分镜）

- 每镜必须输出 `lighting`（具象光影粒子）、`action_continue`（上镜末状态→本镜起始）、`transition`
- description 格式：【目标】事件。【承接】因果。【结果】下镜铺垫
- beat.action 格式：画面 / 动作 / 反应（禁止纯情绪词）
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
