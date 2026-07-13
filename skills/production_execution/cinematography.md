# 分镜运镜与渲染规范（红果/抖音竖屏短剧）

## 1. 运镜指令（camera 字段）

优先**冲击感竖屏短剧运镜**，禁止默认「slow cinematic subtle」。

- **特写/极特写**：face fills 9:16，情绪微表情可读
- **强推近**：fast aggressive dolly push-in（愤怒/震惊）
- **手持**：emotion intensifies → shake increases
- **仰拍**：权力/压迫；**俯拍**：脆弱
- **dolly zoom**：情绪峰值冲击
- 避免：漫长慢推、空镜氛围散文、无主体运镜

战斗、追逐、情绪爆发镜须在 camera 标注 push-in / handheld / impact slow-motion。

## 2. 光影与材质

英文**静帧** prompt 可含 3D 渲染质感（UE5/AO/SSS）。**图生视频 prompt 不要塞这些**，改用竖屏短剧标签：high contrast、rim light、emotional close-up。

## 3. 动态模糊与帧率

- **高速战斗**：motion blur on limbs、motion streaks
- **情绪爆发特写**：impact slow-motion → 切回正常速，定格面部
- **冲击瞬间**：radial motion blur、freeze-frame peak pose
