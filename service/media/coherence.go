package media

import (
	"toonflow/service/storyboard"
	"toonflow/service/internal/fsutil"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// CoherencePassThreshold is the minimum total score (0-100) for a clip to pass QC.
const CoherencePassThreshold = 60.0

// Coherence dimension keys — 14 quantifiable metrics per toonflow.doc §4.5.
var coherenceDimensionKeys = []string{
	"face_continuity",
	"body_consistency",
	"costume_stability",
	"scene_layout",
	"lighting_temporal",
	"color_temporal",
	"motion_smoothness",
	"motion_trajectory",
	"no_jump_stutter",
	"semantic_coherence",
	"transition_natural",
	"style_unity",
	"frame_jitter",
	"anchor_consistency",
}

// CoherenceScore holds multi-dimensional clip quality scores.
type CoherenceScore struct {
	Total      float64            `json:"total"`
	Dimensions map[string]float64 `json:"dimensions"`
	Passed     bool               `json:"passed"`
}

// ScoreVideoClip evaluates a local mp4 against optional source image and shot metadata.
func ScoreVideoClip(ctx context.Context, videoPath, sourceImagePath string, shot *storyboard.ShotMeta) (*CoherenceScore, error) {
	if videoPath == "" {
		return nil, fmt.Errorf("video path required")
	}
	if _, err := os.Stat(videoPath); err != nil {
		return nil, fmt.Errorf("video not found: %w", err)
	}

	workDir, err := os.MkdirTemp("", "coherence_")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(workDir)

	frames, err := extractSampleFrames(ctx, videoPath, workDir, 8)
	if err != nil {
		return nil, err
	}
	if len(frames) < 2 {
		return nil, fmt.Errorf("insufficient frames for coherence scoring")
	}

	imgs := make([]image.Image, 0, len(frames))
	for _, f := range frames {
		img, err := loadImageFile(f)
		if err != nil {
			continue
		}
		imgs = append(imgs, img)
	}
	if len(imgs) < 2 {
		return nil, fmt.Errorf("could not decode frames")
	}

	diffs := consecutiveDiffs(imgs)
	brightness := frameBrightnessSeries(imgs)
	colorSeries := frameColorSeries(imgs)

	dims := make(map[string]float64, len(coherenceDimensionKeys))

	motionSmooth := scoreMotionSmoothness(diffs)
	jitter := scoreLowJitter(diffs)
	noJump := scoreNoJumpStutter(diffs)
	colorStab := scoreColorStability(colorSeries)
	lightStab := scoreBrightnessStability(brightness)
	layoutStab := scoreSceneLayoutStability(imgs)
	anchor := scoreAnchorConsistency(imgs[0], sourceImagePath)

	dims["motion_smoothness"] = motionSmooth
	dims["frame_jitter"] = jitter
	dims["no_jump_stutter"] = noJump
	dims["color_temporal"] = colorStab
	dims["lighting_temporal"] = lightStab
	dims["scene_layout"] = layoutStab
	dims["anchor_consistency"] = anchor
	dims["motion_trajectory"] = (motionSmooth + noJump) / 2

	// Visual identity proxies derived from anchor + temporal stability.
	dims["face_continuity"] = (anchor*0.6 + jitter*0.4)
	dims["body_consistency"] = (anchor*0.5 + motionSmooth*0.3 + layoutStab*0.2)
	dims["costume_stability"] = (colorStab*0.5 + anchor*0.3 + layoutStab*0.2)

	semantic, transition, style := scoreMetadataDimensions(shot)
	dims["semantic_coherence"] = semantic
	dims["transition_natural"] = transition
	dims["style_unity"] = (style*0.5 + colorStab*0.3 + lightStab*0.2)

	total := 0.0
	for _, k := range coherenceDimensionKeys {
		total += dims[k]
	}
	total /= float64(len(coherenceDimensionKeys))

	return &CoherenceScore{
		Total:      roundScore(total),
		Dimensions: roundDimensionMap(dims),
		Passed:     total >= CoherencePassThreshold,
	}, nil
}

// CoherenceScoreJSON serializes a score for DB storage.
func CoherenceScoreJSON(s *CoherenceScore) string {
	if s == nil {
		return "{}"
	}
	b, _ := json.Marshal(s)
	return string(b)
}

// SelectBestScoredClip picks the highest-scoring clip among versions for one shot.
func SelectBestScoredClip(ctx context.Context, outputDir string, clips []ShotClip, shot *storyboard.ShotMeta) (*ShotClip, *CoherenceScore, error) {
	if len(clips) == 0 {
		return nil, nil, fmt.Errorf("no clips to score")
	}
	if len(clips) == 1 {
		return scoreSingleClip(ctx, outputDir, &clips[0], shot)
	}

	var best *ShotClip
	var bestScore *CoherenceScore
	var bestTotal float64 = -1

	for i := range clips {
		c, score, err := scoreSingleClip(ctx, outputDir, &clips[i], shot)
		if err != nil || score == nil {
			continue
		}
		if score.Total > bestTotal {
			bestTotal = score.Total
			best = c
			bestScore = score
		}
	}
	if best == nil {
		c, score, err := scoreSingleClip(ctx, outputDir, &clips[len(clips)-1], shot)
		return c, score, err
	}
	return best, bestScore, nil
}

func scoreSingleClip(ctx context.Context, outputDir string, c *ShotClip, shot *storyboard.ShotMeta) (*ShotClip, *CoherenceScore, error) {
	local, ok := fsutil.PublicURLToLocal(outputDir, c.FileURL)
	if !ok {
		return c, nil, fmt.Errorf("clip file not found")
	}
	sourceLocal := ""
	if c.SourceImageURL != "" {
		if p, ok := fsutil.PublicURLToLocal(outputDir, c.SourceImageURL); ok {
			sourceLocal = p
		}
	}
	score, err := ScoreVideoClip(ctx, local, sourceLocal, shot)
	if err != nil {
		return c, nil, err
	}
	cp := *c
	cp.CoherenceScore = score.Total
	cp.CoherenceJSON = CoherenceScoreJSON(score)
	return &cp, score, nil
}

func extractSampleFrames(ctx context.Context, videoPath, workDir string, n int) ([]string, error) {
	if n < 2 {
		n = 2
	}
	pattern := filepath.Join(workDir, "frame_%03d.png")
	args := []string{
		"-y", "-i", videoPath,
		"-vf", fmt.Sprintf("select='not(mod(n\\,%d))'", maxInt(1, 24/n)),
		"-vsync", "vfr", "-frames:v", fmt.Sprintf("%d", n),
		pattern,
	}
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("extract frames: %w: %s", err, string(out))
	}
	var paths []string
	for i := 1; i <= n; i++ {
		p := filepath.Join(workDir, fmt.Sprintf("frame_%03d.png", i))
		if _, err := os.Stat(p); err == nil {
			paths = append(paths, p)
		}
	}
	if len(paths) == 0 {
		// Fallback: grab first and last frame.
		first := filepath.Join(workDir, "first.png")
		last := filepath.Join(workDir, "last.png")
		for _, spec := range []struct{ seek, dest string }{
			{"0", first},
			{"-0.04", last},
		} {
			args := []string{"-y", "-sseof", spec.seek, "-i", videoPath, "-frames:v", "1", spec.dest}
			if spec.seek == "0" {
				args = []string{"-y", "-i", videoPath, "-frames:v", "1", spec.dest}
			}
			_ = exec.CommandContext(ctx, "ffmpeg", args...).Run()
		}
		for _, p := range []string{first, last} {
			if _, err := os.Stat(p); err == nil {
				paths = append(paths, p)
			}
		}
	}
	return paths, nil
}

func loadImageFile(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	return img, err
}

func consecutiveDiffs(imgs []image.Image) []float64 {
	diffs := make([]float64, 0, len(imgs)-1)
	for i := 1; i < len(imgs); i++ {
		diffs = append(diffs, avgPixelDiff(imgs[i-1], imgs[i]))
	}
	return diffs
}

func avgPixelDiff(a, b image.Image) float64 {
	bounds := a.Bounds()
	if !bounds.Eq(b.Bounds()) {
		b = resizeToMatch(b, bounds.Dx(), bounds.Dy())
	}
	var sum float64
	n := 0
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			c1 := color.RGBAModel.Convert(a.At(x, y)).(color.RGBA)
			c2 := color.RGBAModel.Convert(b.At(x, y)).(color.RGBA)
			sum += math.Abs(float64(c1.R)-float64(c2.R)) +
				math.Abs(float64(c1.G)-float64(c2.G)) +
				math.Abs(float64(c1.B)-float64(c2.B))
			n += 3
		}
	}
	if n == 0 {
		return 0
	}
	return sum / float64(n) / 255.0
}

func resizeToMatch(src image.Image, w, h int) image.Image {
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	sb := src.Bounds()
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			sx := sb.Min.X + x*sb.Dx()/w
			sy := sb.Min.Y + y*sb.Dy()/h
			dst.Set(x, y, src.At(sx, sy))
		}
	}
	return dst
}

func frameBrightnessSeries(imgs []image.Image) []float64 {
	out := make([]float64, len(imgs))
	for i, img := range imgs {
		out[i] = avgBrightness(img)
	}
	return out
}

func avgBrightness(img image.Image) float64 {
	bounds := img.Bounds()
	var sum float64
	n := 0
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			c := color.RGBAModel.Convert(img.At(x, y)).(color.RGBA)
			sum += 0.299*float64(c.R) + 0.587*float64(c.G) + 0.114*float64(c.B)
			n++
		}
	}
	if n == 0 {
		return 0
	}
	return sum / float64(n) / 255.0
}

func frameColorSeries(imgs []image.Image) [][3]float64 {
	out := make([][3]float64, len(imgs))
	for i, img := range imgs {
		bounds := img.Bounds()
		var r, g, b float64
		n := 0
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			for x := bounds.Min.X; x < bounds.Max.X; x++ {
				c := color.RGBAModel.Convert(img.At(x, y)).(color.RGBA)
				r += float64(c.R)
				g += float64(c.G)
				b += float64(c.B)
				n++
			}
		}
		if n > 0 {
			out[i] = [3]float64{r / float64(n), g / float64(n), b / float64(n)}
		}
	}
	return out
}

func scoreMotionSmoothness(diffs []float64) float64 {
	if len(diffs) == 0 {
		return 50
	}
	mean, var_ := meanVariance(diffs)
	// Ideal: moderate motion (0.02-0.15) with low variance.
	if mean < 0.005 {
		return 70 // mostly static but acceptable for locked-off shots
	}
	if mean > 0.35 {
		return clampScore(100 - (mean-0.35)*200)
	}
	return clampScore(100 - var_*800)
}

func scoreLowJitter(diffs []float64) float64 {
	if len(diffs) < 2 {
		return 80
	}
	_, var_ := meanVariance(diffs)
	return clampScore(100 - var_*1200)
}

func scoreNoJumpStutter(diffs []float64) float64 {
	for _, d := range diffs {
		if d > 0.25 {
			return clampScore(100 - (d-0.25)*300)
		}
	}
	return 90
}

func scoreColorStability(series [][3]float64) float64 {
	if len(series) < 2 {
		return 80
	}
	var totalVar float64
	for ch := 0; ch < 3; ch++ {
		vals := make([]float64, len(series))
		for i := range series {
			vals[i] = series[i][ch]
		}
		_, v := meanVariance(vals)
		totalVar += v
	}
	return clampScore(100 - totalVar/50)
}

func scoreBrightnessStability(series []float64) float64 {
	if len(series) < 2 {
		return 80
	}
	_, v := meanVariance(series)
	return clampScore(100 - v*2000)
}

func scoreSceneLayoutStability(imgs []image.Image) float64 {
	if len(imgs) < 2 {
		return 80
	}
	var cornerDiffs []float64
	for i := 1; i < len(imgs); i++ {
		cornerDiffs = append(cornerDiffs, cornerRegionDiff(imgs[i-1], imgs[i]))
	}
	mean, _ := meanVariance(cornerDiffs)
	return clampScore(100 - mean*150)
}

func cornerRegionDiff(a, b image.Image) float64 {
	bounds := a.Bounds()
	w, h := bounds.Dx()/4, bounds.Dy()/4
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	regions := []image.Rectangle{
		image.Rect(bounds.Min.X, bounds.Min.Y, bounds.Min.X+w, bounds.Min.Y+h),
		image.Rect(bounds.Max.X-w, bounds.Min.Y, bounds.Max.X, bounds.Min.Y+h),
	}
	var sum float64
	for _, r := range regions {
		sum += regionDiff(a, b, r)
	}
	return sum / float64(len(regions))
}

func regionDiff(a, b image.Image, r image.Rectangle) float64 {
	var sum float64
	n := 0
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			c1 := color.RGBAModel.Convert(a.At(x, y)).(color.RGBA)
			c2 := color.RGBAModel.Convert(b.At(x, y)).(color.RGBA)
			sum += math.Abs(float64(c1.R)-float64(c2.R)) +
				math.Abs(float64(c1.G)-float64(c2.G)) +
				math.Abs(float64(c1.B)-float64(c2.B))
			n += 3
		}
	}
	if n == 0 {
		return 0
	}
	return sum / float64(n) / 255.0
}

func scoreAnchorConsistency(first image.Image, sourceImagePath string) float64 {
	if sourceImagePath == "" {
		return 75
	}
	src, err := loadImageFile(sourceImagePath)
	if err != nil {
		return 75
	}
	diff := avgPixelDiff(first, src)
	return clampScore(100 - diff*120)
}

func scoreMetadataDimensions(shot *storyboard.ShotMeta) (semantic, transition, style float64) {
	semantic, transition, style = 70, 70, 70
	if shot == nil {
		return
	}
	text := strings.ToLower(strings.Join([]string{
		shot.Description, shot.Prompt, shot.Camera,
		shot.Lighting, shot.ActionContinue, shot.Transition,
	}, " "))
	if strings.Contains(text, "character_id") || strings.Contains(text, "style: consistent") {
		style += 10
		semantic += 5
	}
	if strings.Contains(text, "continuity") || strings.Contains(text, "frame-to-frame") {
		style += 8
	}
	if shot.ActionContinue != "" {
		semantic += 10
		transition += 8
	}
	if shot.Transition != "" {
		transition += 10
	}
	if shot.Lighting != "" {
		style += 5
	}
	return clampScore(semantic), clampScore(transition), clampScore(style)
}

func meanVariance(vals []float64) (mean, variance float64) {
	if len(vals) == 0 {
		return 0, 0
	}
	for _, v := range vals {
		mean += v
	}
	mean /= float64(len(vals))
	for _, v := range vals {
		d := v - mean
		variance += d * d
	}
	variance /= float64(len(vals))
	return mean, variance
}

func clampScore(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}

func roundScore(v float64) float64 {
	return math.Round(v*10) / 10
}

func roundDimensionMap(m map[string]float64) map[string]float64 {
	out := make(map[string]float64, len(m))
	for k, v := range m {
		out[k] = roundScore(v)
	}
	return out
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
