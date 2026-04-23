// Package screenshot 提供截图渲染滤镜链和字幕滤镜辅助函数。

package screenshot

import (
	"fmt"
	"strings"
)

// displayAspectFilter 返回当前截图任务应使用的显示宽高比修正过滤器链。
func (r *screenshotRunner) displayAspectFilter() string {
	if strings.TrimSpace(r.render.AspectChain) != "" {
		return r.render.AspectChain
	}
	return buildDisplayAspectFilter()
}

// bitmapSubtitleTargetSize 会返回位图字幕叠加阶段需要匹配的目标画面尺寸。
func (r *screenshotRunner) bitmapSubtitleTargetSize() (int, int) {
	if r == nil {
		return 0, 0
	}
	if r.media.DisplayWidth > 0 && r.media.DisplayHeight > 0 {
		return r.media.DisplayWidth, r.media.DisplayHeight
	}
	return r.media.VideoWidth, r.media.VideoHeight
}

// hasUsablePGSCanvas 会判断当前是否拿到了可用于全画布叠加的 PGS 画布尺寸。
func (r *screenshotRunner) hasUsablePGSCanvas() bool {
	if r == nil || !r.isPGSSubtitle() {
		return false
	}
	targetWidth, targetHeight := r.bitmapSubtitleTargetSize()
	return r.render.SubtitleCanvasWidth > 0 && r.render.SubtitleCanvasHeight > 0 && targetWidth > 0 && targetHeight > 0
}

// buildPGSSubtitleScaleChain 会按目标画面尺寸缩放 PGS 画布。
func (r *screenshotRunner) buildPGSSubtitleScaleChain() string {
	if !r.hasUsablePGSCanvas() {
		return ""
	}
	targetWidth, targetHeight := r.bitmapSubtitleTargetSize()
	if targetWidth == r.render.SubtitleCanvasWidth && targetHeight == r.render.SubtitleCanvasHeight {
		return ""
	}
	return fmt.Sprintf("scale=%d:%d", targetWidth, targetHeight)
}

// pgsOverlayPosition 会返回当前 PGS 叠加使用的位置表达式。
func (r *screenshotRunner) pgsOverlayPosition() string {
	if r.hasUsablePGSCanvas() {
		return "0:0"
	}
	return "(W-w)/2:(H-h-10)"
}

// buildPGSOverlayFilterComplex 会构造“先处理视频，再叠加 PGS”的滤镜图。
func (r *screenshotRunner) buildPGSOverlayFilterComplex(videoChain, overlayTail string) string {
	steps := []string{
		buildFilterGraphStep("[0:v:0]", videoChain, "[video]"),
		buildFilterGraphStep(fmt.Sprintf("[0:s:%d]", r.subtitle.RelativeIndex), r.buildPGSSubtitleScaleChain(), "[sub]"),
		buildFilterGraphStep("[video][sub]", joinFilters(fmt.Sprintf("overlay=%s", r.pgsOverlayPosition()), overlayTail), "[out]"),
	}
	return strings.Join(steps, ";")
}

// buildPGSRenderFilterComplex 会构造截图主流程使用的 PGS 叠加滤镜图。
func (r *screenshotRunner) buildPGSRenderFilterComplex() string {
	return r.buildPGSOverlayFilterComplex(joinFilters(r.render.ColorChain, r.displayAspectFilter()), "")
}

// buildFilterGraphStep 会为 filter_complex 生成单个具名步骤。
func buildFilterGraphStep(input, chain, output string) string {
	filterChain := strings.TrimSpace(chain)
	if filterChain == "" {
		filterChain = "null"
	}
	return fmt.Sprintf("%s%s%s", input, filterChain, output)
}

// buildTextSubtitleRenderChain 会让文字字幕渲染与视频选帧共享同一条绝对时间轴。
func (r *screenshotRunner) buildTextSubtitleRenderChain(timelineBase, aligned float64, subFilter string) string {
	baseTimeline := fmt.Sprintf("setpts=PTS-STARTPTS+%s/TB", formatFloat(timelineBase))
	selectFrame := fmt.Sprintf("select='gte(t,%s)'", formatFloat(aligned))
	if r.usesLibplaceboColorspace() {
		return joinFilters(
			baseTimeline,
			selectFrame,
			r.render.ColorChain,
			subFilter,
			r.displayAspectFilter(),
		)
	}
	return joinFilters(
		baseTimeline,
		selectFrame,
		subFilter,
		r.render.ColorChain,
		r.displayAspectFilter(),
	)
}

// buildTextSubtitleFilter 构建 ffmpeg 文本字幕过滤器，适配外挂字幕和内封文字字幕两种场景。
func (r *screenshotRunner) buildTextSubtitleFilter() string {
	if r.subtitle.Mode == "none" {
		return ""
	}

	sizePart := ""
	if r.media.VideoWidth > 0 && r.media.VideoHeight > 0 {
		sizePart = fmt.Sprintf(":original_size=%dx%d", r.media.VideoWidth, r.media.VideoHeight)
	}
	fontPart := ""
	if strings.TrimSpace(r.subtitleState.SubtitleFontDir) != "" {
		fontPart = fmt.Sprintf(":fontsdir='%s'", escapeFilterValue(r.subtitleState.SubtitleFontDir))
	}

	switch r.subtitle.Mode {
	case "external":
		return fmt.Sprintf("subtitles='%s'%s%s", escapeFilterValue(r.subtitle.File), sizePart, fontPart)
	case "internal":
		return fmt.Sprintf("subtitles='%s'%s%s:si=%d", escapeFilterValue(r.sourcePath), sizePart, fontPart, r.subtitle.RelativeIndex)
	default:
		return ""
	}
}

// bitmapSubtitleKind 返回当前字幕 codec 对应的位图字幕类型。
func (r *screenshotRunner) bitmapSubtitleKind() bitmapSubtitleKind {
	return bitmapSubtitleKindFromCodec(r.subtitle.Codec)
}

// isPGSSubtitle 会判断PGS字幕是否满足当前条件。
func (r *screenshotRunner) isPGSSubtitle() bool {
	return r.bitmapSubtitleKind() == bitmapSubtitlePGS
}

// isDVDSubtitle 会判断DVD字幕是否满足当前条件。
func (r *screenshotRunner) isDVDSubtitle() bool {
	return r.bitmapSubtitleKind() == bitmapSubtitleDVD
}

// isSupportedBitmapSubtitle 会判断受支持位图字幕是否满足当前条件。
func (r *screenshotRunner) isSupportedBitmapSubtitle() bool {
	return r.isPGSSubtitle() || r.isDVDSubtitle()
}

// requiresTextSubtitleFilter 会判断当前截图场景是否需要走 subtitles 文字滤镜。
func (r *screenshotRunner) requiresTextSubtitleFilter() bool {
	if r == nil || r.subtitle.Mode == "none" {
		return false
	}
	if r.subtitle.Mode == "external" {
		return true
	}
	if r.subtitle.Mode == "internal" && !r.isSupportedBitmapSubtitle() {
		return true
	}
	return false
}

// bitmapSubtitleKindFromCodec 把 codec 名称映射到内部使用的位图字幕类型枚举。
func bitmapSubtitleKindFromCodec(codec string) bitmapSubtitleKind {
	switch strings.ToLower(strings.TrimSpace(codec)) {
	case "hdmv_pgs_subtitle", "pgssub":
		return bitmapSubtitlePGS
	case "dvd_subtitle":
		return bitmapSubtitleDVD
	default:
		return bitmapSubtitleNone
	}
}

// isUnsupportedBitmapSubtitleCodec 会判断Unsupported位图字幕Codec是否满足当前条件。
func isUnsupportedBitmapSubtitleCodec(codec string) bool {
	switch strings.ToLower(strings.TrimSpace(codec)) {
	case "dvb_subtitle", "xsub", "vobsub":
		return true
	default:
		return false
	}
}
