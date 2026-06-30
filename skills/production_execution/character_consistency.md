# 角色一致性与风格锚点

## 角色一致性（extract_assets + generate_storyboard）

- 主要角色须生成 **Turnaround Sheet（角色设定卡）**：正面 / 侧面 / 背面 / 3/4 视角文字描述
- 每个角色资产写明：`character_id`、不可变 `feature_keywords`（发型、服装、配色、体型）
- 分镜每镜须：
  - `asset_ids` 引用资产库 ID
  - `prompt` 含 `character_id: [name], style: consistent` 及该镜出现角色的特征关键词
  - 不得凭空改写角色外貌

## 风格化渲染锚点（分镜 prompt）

英文 prompt 须包含引擎与一致性术语（按项目画风选用）：
- `Unreal Engine 5 render` / `Octane Render`
- `high fidelity`, `consistent lighting`, `consistent character design`
- 明确画幅：`16:9 widescreen` 或 `9:16 vertical`
- 色调范围：如 `teal-orange cinematic grade`, `controlled saturation`, `unified color palette`

参考图构图与色调须与项目 `video_ratio`、画风一致。
