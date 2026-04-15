// Package screenshot 验证截图过滤器拼接逻辑的关键回归场景。

package screenshot

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// TestBuildTextSubtitleFilterForInternalTextSubtitle 验证内封文字字幕过滤器会保持与 shell 一致的 si 写法。
func TestBuildTextSubtitleFilterForInternalTextSubtitle(t *testing.T) {
	runner := &screenshotRunner{
		sourcePath:  "/media/example/video.mkv",
		videoWidth:  1920,
		videoHeight: 1080,
		subtitle: subtitleSelection{
			Mode:          "internal",
			RelativeIndex: 1,
		},
	}

	filter := runner.buildTextSubtitleFilter()
	if !strings.Contains(filter, "subtitles='/media/example/video.mkv'") {
		t.Fatalf("expected shell-style subtitles path in filter, got %q", filter)
	}
	if !strings.Contains(filter, ":si=1") {
		t.Fatalf("expected shell-style si option in filter, got %q", filter)
	}
	if !strings.Contains(filter, ":original_size=1920x1080") {
		t.Fatalf("expected original_size in filter, got %q", filter)
	}
}

// TestBuildTextSubtitleFilterForExternalSubtitle 验证外挂文字字幕过滤器也会保持 shell 的位置参数写法。
func TestBuildTextSubtitleFilterForExternalSubtitle(t *testing.T) {
	runner := &screenshotRunner{
		videoWidth:  1280,
		videoHeight: 720,
		subtitle: subtitleSelection{
			Mode: "external",
			File: "/media/example/subtitle.srt",
		},
	}

	filter := runner.buildTextSubtitleFilter()
	if !strings.Contains(filter, "subtitles='/media/example/subtitle.srt'") {
		t.Fatalf("expected shell-style subtitles path in filter, got %q", filter)
	}
	if strings.Contains(filter, ":si=") {
		t.Fatalf("did not expect si for external subtitle, got %q", filter)
	}
}

func TestBuildTextSubtitleFilterIncludesFontsDirWhenPrepared(t *testing.T) {
	runner := &screenshotRunner{
		sourcePath:      "/media/example/video.mkv",
		videoWidth:      1920,
		videoHeight:     1080,
		subtitleFontDir: "/tmp/minfo-sub-fonts-123",
		subtitle: subtitleSelection{
			Mode:          "internal",
			RelativeIndex: 1,
		},
	}

	filter := runner.buildTextSubtitleFilter()
	if !strings.Contains(filter, ":fontsdir='/tmp/minfo-sub-fonts-123'") {
		t.Fatalf("expected fontsdir in filter, got %q", filter)
	}
}

func TestBuildPGSRenderFilterComplexAppliesVideoProcessingBeforeOverlay(t *testing.T) {
	runner := &screenshotRunner{
		colorChain:           "libplacebo=colorspace=gbr",
		aspectChain:          "setsar=1",
		displayWidth:         3840,
		displayHeight:        2160,
		subtitleCanvasWidth:  1920,
		subtitleCanvasHeight: 1080,
		subtitle: subtitleSelection{
			Mode:          "internal",
			RelativeIndex: 2,
			Codec:         "hdmv_pgs_subtitle",
		},
	}

	filter := runner.buildPGSRenderFilterComplex()
	if !strings.Contains(filter, "[0:v:0]libplacebo=colorspace=gbr,setsar=1[video]") {
		t.Fatalf("expected video processing before overlay, got %q", filter)
	}
	if !strings.Contains(filter, "[0:s:2]scale=3840:2160[sub]") {
		t.Fatalf("expected PGS canvas scaling step, got %q", filter)
	}
	if !strings.Contains(filter, "[video][sub]overlay=0:0[out]") {
		t.Fatalf("expected full-canvas overlay position, got %q", filter)
	}
}

func TestBuildPGSRenderFilterComplexFallsBackWhenCanvasUnknown(t *testing.T) {
	runner := &screenshotRunner{
		aspectChain: "setsar=1",
		subtitle: subtitleSelection{
			Mode:          "internal",
			RelativeIndex: 1,
			Codec:         "hdmv_pgs_subtitle",
		},
	}

	filter := runner.buildPGSRenderFilterComplex()
	if !strings.Contains(filter, "[0:s:1]null[sub]") {
		t.Fatalf("expected subtitle null step when canvas is unknown, got %q", filter)
	}
	if !strings.Contains(filter, "overlay=(W-w)/2:(H-h-10)") {
		t.Fatalf("expected legacy overlay fallback when canvas is unknown, got %q", filter)
	}
}

func TestRequiresTextSubtitleFilterForExternalSubtitle(t *testing.T) {
	runner := &screenshotRunner{
		subtitle: subtitleSelection{
			Mode: "external",
			File: "/media/example/subtitle.srt",
		},
	}

	if !runner.requiresTextSubtitleFilter() {
		t.Fatal("expected external text subtitle to require text subtitle filter")
	}
}

func TestRequiresTextSubtitleFilterForBitmapSubtitle(t *testing.T) {
	runner := &screenshotRunner{
		subtitle: subtitleSelection{
			Mode:  "internal",
			Codec: "hdmv_pgs_subtitle",
		},
	}

	if runner.requiresTextSubtitleFilter() {
		t.Fatal("expected bitmap subtitle to avoid text subtitle filter")
	}
}

func TestRenderCoarseBackUsesDedicatedBitmapWindow(t *testing.T) {
	runner := &screenshotRunner{
		settings: variantSettings{
			CoarseBackPGS:  12,
			RenderBackPGS:  2,
			CoarseBackText: 3,
			RenderBackText: 1,
		},
		subtitle: subtitleSelection{
			Mode:  "internal",
			Codec: "hdmv_pgs_subtitle",
		},
	}

	if got := runner.renderCoarseBack(); got != 2 {
		t.Fatalf("renderCoarseBack() = %d, want 2 for bitmap subtitle render path", got)
	}
}

func TestRenderCoarseBackUsesBitmapOverrideWhenPresent(t *testing.T) {
	runner := &screenshotRunner{
		settings: variantSettings{
			CoarseBackPGS:  12,
			RenderBackPGS:  2,
			CoarseBackText: 3,
			RenderBackText: 1,
		},
		subtitle: subtitleSelection{
			Mode:  "internal",
			Codec: "hdmv_pgs_subtitle",
		},
		bitmapRenderBackOverride: 12,
	}

	if got := runner.renderCoarseBack(); got != 12 {
		t.Fatalf("renderCoarseBack() = %d, want 12 after bitmap override", got)
	}
}

// TestShellStyleTextSubtitleChain 验证文字字幕过滤器链保持 shell 的 setpts 后接 subtitles 顺序。
func TestShellStyleTextSubtitleChain(t *testing.T) {
	filter := joinFilters(
		"setpts=PTS-STARTPTS+61.000/TB",
		"subtitles='/media/example/video.mkv':original_size=3840x2160:si=1",
	)

	expected := "setpts=PTS-STARTPTS+61.000/TB,subtitles='/media/example/video.mkv':original_size=3840x2160:si=1"
	if filter != expected {
		t.Fatalf("expected shell-style filter chain %q, got %q", expected, filter)
	}
}

func TestBuildTextSubtitleRenderChainUsesLibplaceboBeforeSubtitles(t *testing.T) {
	runner := &screenshotRunner{
		colorChain: "libplacebo=colorspace=gbr",
	}

	filter := runner.buildTextSubtitleRenderChain(61, "subtitles='/media/example/video.mkv':original_size=3840x2160:si=1")

	expected := "libplacebo=colorspace=gbr,setpts=PTS-STARTPTS+61.000/TB,subtitles='/media/example/video.mkv':original_size=3840x2160:si=1"
	if filter != expected {
		t.Fatalf("expected libplacebo-first text subtitle chain %q, got %q", expected, filter)
	}
}

func TestIsLibplaceboRenderCrashMessage(t *testing.T) {
	messages := []string{
		"LLVM ERROR: Cannot select: ...\nIn function: cs_co_variant.resume",
		`Assertion failed: !"unlinking orphaned child?" (../src/pl_alloc.c: unlink_child: 115)`,
		"signal: segmentation fault (core dumped)",
	}

	for _, message := range messages {
		if !isLibplaceboRenderCrashMessage(message) {
			t.Fatalf("expected libplacebo render crash message to be detected for %q", message)
		}
	}
}

func TestApplyLibplaceboRenderFallbackSwitchesToCompatibleChain(t *testing.T) {
	runner := &screenshotRunner{
		libplaceboReady: true,
		colorInfo:       "color_primaries=bt2020|color_space=bt2020nc|color_transfer=smpte2084|",
		colorChain:      "libplacebo=colorspace=gbr",
	}

	changed := runner.applyLibplaceboRenderFallback(fmt.Errorf("LLVM ERROR: Cannot select: ..."))
	if !changed {
		t.Fatal("expected libplacebo fallback to trigger")
	}
	if runner.libplaceboReady {
		t.Fatal("expected libplaceboReady to be disabled after fallback")
	}
	if strings.Contains(runner.colorChain, "libplacebo=") {
		t.Fatalf("expected fallback colorspace chain to avoid libplacebo, got %q", runner.colorChain)
	}
	if !strings.Contains(runner.colorChain, "tonemap=mobius") {
		t.Fatalf("expected fallback colorspace chain to use tonemap path, got %q", runner.colorChain)
	}
}

func TestApproximateRenderProgressPercentFromSpeed(t *testing.T) {
	state := &ffmpegRealtimeState{
		startedAt:     time.Now().Add(-2 * time.Second),
		windowSeconds: 4,
		speed:         "2.0x",
	}

	percent, ok := approximateRenderProgressPercent(state)
	if !ok {
		t.Fatal("approximateRenderProgressPercent returned ok=false")
	}
	if percent < 90 || percent > 94 {
		t.Fatalf("percent = %.1f, want between 90 and 94", percent)
	}
}

func TestApproximateRenderProgressPercentFromLocalOutTime(t *testing.T) {
	state := &ffmpegRealtimeState{
		windowSeconds:   8,
		firstOutTimeMS:  2_000_000,
		outTimeMS:       5_000_000,
		hasFirstOutTime: true,
	}

	percent, ok := approximateRenderProgressPercent(state)
	if !ok {
		t.Fatal("approximateRenderProgressPercent returned ok=false")
	}
	if percent != 37.5 {
		t.Fatalf("percent = %.1f, want 37.5", percent)
	}
}

func TestApproximateRenderProgressPercentFromElapsedFallback(t *testing.T) {
	state := &ffmpegRealtimeState{
		startedAt:     time.Now().Add(-2 * time.Second),
		windowSeconds: 0,
	}

	percent, ok := approximateRenderProgressPercent(state)
	if !ok {
		t.Fatal("approximateRenderProgressPercent returned ok=false")
	}
	if percent < 50 || percent > 65 {
		t.Fatalf("percent = %.1f, want between 50 and 65", percent)
	}
}

func TestFFmpegSubtitleProgressPercentNormalizesFromFirstSubtitleTimestamp(t *testing.T) {
	runner := &screenshotRunner{
		duration: 7200,
	}
	state := &ffmpegRealtimeState{
		outTimeMS:       3_900_000_000,
		firstOutTimeMS:  3_600_000_000,
		hasFirstOutTime: true,
	}

	percent := runner.ffmpegProgressPercent("字幕", "continue", state)
	if percent < 8 || percent > 9 {
		t.Fatalf("percent = %.2f, want between 8 and 9 after normalizing from first subtitle timestamp", percent)
	}
}

func TestNormalizeRenderProgressWindow(t *testing.T) {
	tests := []struct {
		input float64
		want  float64
	}{
		{input: 0, want: 0.5},
		{input: 0.2, want: 0.5},
		{input: 1.5, want: 1.5},
	}

	for _, tt := range tests {
		if got := normalizeRenderProgressWindow(tt.input); got != tt.want {
			t.Fatalf("normalizeRenderProgressWindow(%v) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestBuildColorspaceChainForHDR(t *testing.T) {
	info := "color_primaries=bt2020|color_space=bt2020nc|color_transfer=smpte2084|"
	chain := buildColorspaceChain(info, true)

	if !strings.Contains(chain, "libplacebo=") {
		t.Fatalf("expected HDR colorspace chain to use libplacebo, got %q", chain)
	}
	if !strings.Contains(chain, "colorspace=gbr") {
		t.Fatalf("expected HDR colorspace chain to tag RGB output as gbr, got %q", chain)
	}
	if !strings.Contains(chain, "peak_detect=false") {
		t.Fatalf("expected HDR colorspace chain to disable peak detection in conservative llvmpipe mode, got %q", chain)
	}
	if !strings.Contains(chain, "color_trc=iec61966-2-1") {
		t.Fatalf("expected HDR colorspace chain to target sRGB transfer, got %q", chain)
	}
	if strings.Contains(chain, "colorspace=bt709") {
		t.Fatalf("did not expect HDR colorspace chain to tag rgb24 output as bt709, got %q", chain)
	}
	if strings.Contains(chain, "apply_dolbyvision=true") {
		t.Fatalf("did not expect non-DV HDR chain to force Dolby Vision processing, got %q", chain)
	}
}

func TestBuildColorspaceChainForDolbyVision(t *testing.T) {
	info := "color_primaries=bt2020|color_space=bt2020nc|color_transfer=smpte2084|dolby_vision=1|dv_profile=8|"
	chain := buildColorspaceChain(info, true)

	if !strings.Contains(chain, "libplacebo=") {
		t.Fatalf("expected Dolby Vision colorspace chain to use libplacebo, got %q", chain)
	}
	if !strings.Contains(chain, "colorspace=gbr") {
		t.Fatalf("expected Dolby Vision colorspace chain to tag RGB output as gbr, got %q", chain)
	}
	if !strings.Contains(chain, "apply_dolbyvision=true") {
		t.Fatalf("expected Dolby Vision colorspace chain to apply Dolby Vision metadata, got %q", chain)
	}
	if !strings.Contains(chain, "peak_detect=false") {
		t.Fatalf("expected Dolby Vision colorspace chain to disable peak detection in conservative llvmpipe mode, got %q", chain)
	}
}

func TestBuildColorspaceChainForSDR(t *testing.T) {
	info := "color_primaries=bt709|color_space=bt709|color_transfer=bt709|"
	chain := buildColorspaceChain(info, true)

	if chain != "" {
		t.Fatalf("expected SDR colorspace chain to be empty, got %q", chain)
	}
}

func TestBuildColorspaceChainFallbackWithoutLibplacebo(t *testing.T) {
	info := "color_primaries=bt2020|color_space=bt2020nc|color_transfer=smpte2084|"
	chain := buildColorspaceChain(info, false)

	if !strings.Contains(chain, "tonemap=mobius") {
		t.Fatalf("expected fallback HDR colorspace chain to use existing zscale/tonemap path, got %q", chain)
	}
	if strings.Contains(chain, "libplacebo=") {
		t.Fatalf("did not expect fallback HDR colorspace chain to use libplacebo, got %q", chain)
	}
}

func TestBuildDisplayAspectFilterForMetadataUsesDARForAnamorphicDVD(t *testing.T) {
	filter := buildDisplayAspectFilterForMetadata(720, 480, "32:27", "16:9")
	if filter != "scale='trunc(ih*16/9/2)*2:ih',setsar=1" {
		t.Fatalf("filter = %q, want DAR-based widescreen expansion", filter)
	}
}

func TestBuildDisplayAspectFilterForMetadataKeepsSquarePixelVideo(t *testing.T) {
	filter := buildDisplayAspectFilterForMetadata(1920, 1080, "1:1", "16:9")
	if filter != "setsar=1" {
		t.Fatalf("filter = %q, want setsar-only for square-pixel video", filter)
	}
}

func TestBuildDisplayAspectFilterForMetadataSupportsMediaInfoFloatDAR(t *testing.T) {
	filter := buildDisplayAspectFilterForMetadata(720, 480, "", "1.778")
	if filter != "scale='trunc(ih*16/9/2)*2:ih',setsar=1" {
		t.Fatalf("filter = %q, want MediaInfo float DAR to normalize to exact 16:9 expansion", filter)
	}
}

func TestDetectDisplayDimensionsForMetadataUsesDAR(t *testing.T) {
	width, height := detectDisplayDimensionsForMetadata(720, 480, "8:9", "16:9")
	if width != 852 || height != 480 {
		t.Fatalf("detectDisplayDimensionsForMetadata() = %dx%d, want 852x480", width, height)
	}
}

func TestNormalizeMediaInfoAspectRatioMapsCommonWidescreenValue(t *testing.T) {
	ratio := normalizeMediaInfoAspectRatio("1.778")
	if ratio != "16:9" {
		t.Fatalf("ratio = %q, want 16:9", ratio)
	}
}

func TestNormalizeMediaInfoAspectRatioMapsCommonFullscreenValue(t *testing.T) {
	ratio := normalizeMediaInfoAspectRatio("1.333")
	if ratio != "4:3" {
		t.Fatalf("ratio = %q, want 4:3", ratio)
	}
}

func TestBuildOxiPNGCompressionArgs(t *testing.T) {
	args := buildOxiPNGCompressionArgs("/tmp/input.png")
	joined := strings.Join(args, " ")

	if !strings.Contains(joined, "-o max") {
		t.Fatalf("expected max optimization level in oxipng args, got %q", joined)
	}
	if !strings.Contains(joined, "--strip safe") {
		t.Fatalf("expected safe metadata stripping in oxipng args, got %q", joined)
	}
	if !strings.HasSuffix(joined, "/tmp/input.png") {
		t.Fatalf("expected input path in oxipng args, got %q", joined)
	}
}

func TestBuildPNGQuantCompressionArgs(t *testing.T) {
	args := buildPNGQuantCompressionArgs("/tmp/input.png", "/tmp/output.png")
	joined := strings.Join(args, " ")

	if !strings.Contains(joined, "256") {
		t.Fatalf("expected 256-color target in pngquant args, got %q", joined)
	}
	if !strings.Contains(joined, "--output /tmp/output.png") {
		t.Fatalf("expected explicit output path in pngquant args, got %q", joined)
	}
	if !strings.Contains(joined, "--strip") {
		t.Fatalf("expected metadata stripping in pngquant args, got %q", joined)
	}
	if !strings.HasSuffix(joined, "-- /tmp/input.png") {
		t.Fatalf("expected input path after arg separator in pngquant args, got %q", joined)
	}
}
