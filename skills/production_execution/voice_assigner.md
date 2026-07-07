# 角色音色分配

为短剧角色分配 Edge 神经语音，保证同一角色全剧音色一致。

## 规则

1. 主要角色必须分配不同音色，避免听众混淆
2. 根据角色 desc 中的性别、年龄、气质选择：
   - 年轻女性 → zh-CN-XiaoxiaoNeural 或 zh-CN-XiaoyiNeural
   - 成熟女性 → zh-CN-XiaohanNeural
   - 年轻男性 → zh-CN-YunxiNeural
   - 沉稳/反派男性 → zh-CN-YunjianNeural
   - 少年/孩童 → zh-CN-YunxiaNeural
   - 旁白/解说 → zh-CN-YunyangNeural
3. 配角可与主角音色区分即可，同性别配角勿与主角重复
4. 仅输出 JSON 数组，字段：name（角色名）、voice_id（完整 Edge voice ID）
