# 分镜运镜与渲染规范（红果/抖音竖屏短剧）

## 1. 运镜指令（camera 字段）

优先**竖屏可执行运镜**，禁止「slow cinematic」「强化情绪」这类抽象说法。

- **特写/极特写**：脸或五官填满 9:16，眉/眼/唇有位移
- **强推近**：dolly push-in 直到脸颊贴边
- **手持**：人物踏前时机身微抖加大
- **仰拍**：镜头低于下巴往上；**俯拍**：镜头高于头顶往下
- **dolly zoom**：脸相对大小不变、背景收缩/扩张
- 避免：漫长空镜、无主体运镜

打架、追逐、重拍面必须标注：push-in / handheld / impact slow-motion。

## 2. 光影与材质

英文**静帧** prompt 可含 3D 渲染质感（UE5/AO/SSS）。**图生视频**不要塞这些，改用：high contrast、rim light on cheek、face fills frame。

## 3. 动态模糊与帧率

- **高速动作**：四肢 motion blur、streaks
- **重拍面**：impact slow-motion 一拍 → 切回正常速 → 定格姿态
- **冲击瞬间**：radial blur → freeze-frame peak pose
