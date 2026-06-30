# 分镜运镜与渲染规范（3D 动漫）

## 1. 运镜指令（camera 字段）

不得只写「推镜/摇镜」等笼统词。每镜须写明具体运镜参数，中文简述 + 英文术语：

- **推拉**：dolly in / dolly out / dolly zoom（希区库克变焦）
- **摇移**：pan left/right、tilt up/down、tracking shot、crane up
- **焦点**：rack focus（跟焦）、shallow depth of field、deep focus
- **速度**：slow motion 48–120fps、speed ramp、locked-off static
- **情绪镜头**：handheld shake、low angle heroic、high angle vulnerable

战斗、追逐、情绪爆发镜须在 camera 或 description 标注 slow motion 或 motion blur 强度暗示。

## 2. 光影与材质（prompt 字段）

英文 prompt 须体现 3D 动漫物理质感，按场景选用：

- **环境**：ambient occlusion (AO)、contact shadows、global illumination
- **体积光**：volumetric god rays、atmospheric scattering、light shafts
- **皮肤**：subsurface scattering (SSS)、translucent skin、soft rim light
- **硬表面**：PBR metallic reflectivity、anisotropic highlights on armor/weapons
- **氛围**：cinematic color grading、rim light、bounce light

## 3. 动态模糊与帧率（高速/情绪场景）

以下场景须在 prompt 中标注动态表现：

- **高速战斗/追逐**：motion blur on limbs/weapons、motion streaks、high shutter action
- **情绪爆发特写**：slow motion close-up 120fps、micro-expression detail、tear/blood particles
- **冲击瞬间**：radial motion blur、freeze-frame peak pose with blur trails

prompt 与 camera 字段术语须一致，避免矛盾。
