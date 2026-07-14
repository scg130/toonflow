# I2V / 图生视频提示词技能

正文由代码拼装分镜动作；本文件只提供可热改的锁句、风格标签与负向词。

## positive_locks
- image1 is first frame lock, imageN is last frame target
- generate only continuous motion between locked frames
- preserve subject identity, face structure, outfit, hairstyle, and scene layout
- do not redesign character or room; no new objects unless the action requires them

## motion_tail
- one physical action path, no hard cuts inside the clip
- end pose must land on the last keyframe
- brows lids lips and jaw move; shoulders and hands shift clearly
- this clip only — do not jump ahead to later story beats

## clip_tail
- silent video no generated speech
- Chinese drama visuals only
- smooth temporal interpolation
- frame-to-frame continuity
- clear brows lids lips and hand motion

## style_tags
- Chinese vertical short drama style
- Hongguo Douyin short-series look
- 9:16 vertical framing
- tight face fill vertical frame
- high contrast punchy color
- side rim light on cheek edge
- brows lids lips move on cue
- fast readable body beats
- commercial short-drama production value

## mode_frames2
FLF2V two-frame morph first-to-last only

## mode_multiframe
multi-keyframe continuous action take

## camera_default_human
one slow vertical short-drama push-in on face

## camera_default_prop
locked or one motivated vertical short-drama camera move

## continuity_accepted_prefix
first image is the accepted previous-clip ending — begin exactly from that pose and layout; preserve face identity, outfit, hairstyle, and scene layout; generate only the continuous transition toward the last keyframe

## dialogue_line
%s近景张嘴说短句，下颌开合清晰：%s
%s唇形随字咬合开合，眉头与下颌同步位移

## dialogue_tail
- 仅口型与肢体表演，视频禁止生成任何语音
- 无声画面，不要英文对白音频

## non_human_tail
no human character motion, object and environment only

## negative
static image, frozen frame, slideshow, still photo, no motion, boring slow motion, soft dreamy cinematic essay, empty atmosphere shot, vague mood without action, morphing, warping, flickering, jitter, stuttering, low fps, blurry, out of focus, low quality, low resolution, distorted face, deformed body, bad anatomy, extra limbs, identity drift, face swap mid-clip, outfit change, hairstyle change, room redesign, background swap, new character appear, watermark, text overlay, logo, subtitle, random color shift, style drift, temporal discontinuity, jump cut, English speech, English dialogue, foreign language audio, voiceover, narration, spoken words, talking audio, action freeze mid-motion, discontinuous movement, overstacked VFX particles without story, generic fantasy MV montage, ignore last keyframe, drift away from keyframe poses, cinematic, epic, beautiful, dramatic, stunning, breathtaking, dynamic, atmospheric, magical, professional, masterpiece, 8K, ultra HD, high quality, trending on ArtStation

## negative_lip_sync
closed mouth while speaking, static lips during dialogue, no lip sync, mute expression while talking, wrong speaker lip movement

## anti_slop
电影感, 氛围感, 史诗感, 戏剧性, 震撼, 唯美, 大气, 高级感, cinematic, epic, dramatic, beautiful, stunning, breathtaking, dynamic, atmospheric, magical, masterpiece

## literary_mood_only
悲愤欲绝, 几近破碎, 情绪崩溃, 滔天怒火, 杀意沸腾, 心境崩塌, 威压, 神念, 气势逼人, 无风起浪, 氛围压抑, 沉重气氛

## conflict_hints
冲突, 打脸, 反转, 撕, 砸, 跪下, 冲, 打, 爆, 围攻, 对峙, 追, 杀, 怒吼, 一拳, 战斗, 打斗, 高潮, push-in, handheld, dolly zoom, slow-motion, 慢放, 手持, 急速
