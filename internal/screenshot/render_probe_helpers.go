// Package screenshot 提供截图流程中的媒体与渲染参数探测辅助函数。

package screenshot

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	screenshotdvdinfo "minfo/internal/screenshot/dvdinfo"
	screenshotsource "minfo/internal/screenshot/source"
	screenshottimestamps "minfo/internal/screenshot/timestamps"
	"minfo/internal/system"
)

// detectStartOffset 读取当前截图源的起始时间偏移。
func (r *screenshotRunner) detectStartOffset() float64 {
	return r.detectStartOffsetForInput(r.sourcePath)
}

// detectStartOffsetForInput 读取指定输入的起始时间偏移。
func (r *screenshotRunner) detectStartOffsetForInput(input string) float64 {
	stdout, _, err := system.RunCommand(r.ctx, r.tools.FFprobeBin,
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=start_time",
		"-of", "default=noprint_wrappers=1:nokey=1",
		input,
	)
	if err == nil {
		if value, ok := firstFloatLine(stdout); ok {
			return value
		}
	}

	stdout, _, err = system.RunCommand(r.ctx, r.tools.FFprobeBin,
		"-v", "error",
		"-show_entries", "format=start_time",
		"-of", "default=noprint_wrappers=1:nokey=1",
		input,
	)
	if err == nil {
		if value, ok := firstFloatLine(stdout); ok {
			return value
		}
	}
	return 0
}

// detectVideoDimensions 读取当前视频流的宽高。
func (r *screenshotRunner) detectVideoDimensions() (int, int) {
	stdout, _, err := system.RunCommand(r.ctx, r.tools.FFprobeBin,
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=width,height",
		"-of", "csv=p=0:s=x",
		r.sourcePath,
	)
	if err != nil {
		return 0, 0
	}

	value := strings.TrimSpace(strings.SplitN(stdout, "\n", 2)[0])
	parts := strings.Split(value, "x")
	if len(parts) != 2 {
		return 0, 0
	}

	width, err1 := strconv.Atoi(parts[0])
	height, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return 0, 0
	}
	return width, height
}

// detectBitmapSubtitleCanvasDimensions 会读取当前位图字幕流声明的画布尺寸。
func (r *screenshotRunner) detectBitmapSubtitleCanvasDimensions() (int, int) {
	if r == nil || r.subtitle.Mode != "internal" || !r.isSupportedBitmapSubtitle() || r.subtitle.RelativeIndex < 0 {
		return 0, 0
	}

	stdout, _, err := system.RunCommand(r.ctx, r.tools.FFprobeBin,
		"-v", "error",
		"-select_streams", fmt.Sprintf("s:%d", r.subtitle.RelativeIndex),
		"-show_entries", "stream=width,height",
		"-of", "csv=p=0:s=x",
		r.sourcePath,
	)
	if err != nil {
		return 0, 0
	}

	value := strings.TrimSpace(strings.SplitN(stdout, "\n", 2)[0])
	parts := strings.Split(value, "x")
	if len(parts) != 2 {
		return 0, 0
	}

	width, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
	height, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err1 != nil || err2 != nil || width <= 0 || height <= 0 {
		return 0, 0
	}
	return width, height
}

// detectColorspace 读取视频流的色彩空间元数据，并整理成稳定键值。
func (r *screenshotRunner) detectColorspace() string {
	stdout, _, err := system.RunCommand(r.ctx, r.tools.FFprobeBin,
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=color_space,color_primaries,color_transfer:stream_side_data=side_data_type,dv_profile",
		"-of", "json",
		r.sourcePath,
	)
	if err == nil {
		if info := parseColorspaceProbeJSON(stdout); info != "" {
			return info
		}
	}

	stdout, _, err = system.RunCommand(r.ctx, r.tools.FFprobeBin,
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=color_space,color_primaries,color_transfer",
		"-of", "default=noprint_wrappers=1",
		r.sourcePath,
	)
	if err != nil {
		return ""
	}

	lines := make([]string, 0, 3)
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "color_space=") || strings.HasPrefix(line, "color_primaries=") || strings.HasPrefix(line, "color_transfer=") {
			lines = append(lines, line)
		}
	}
	sort.Strings(lines)
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "|") + "|"
}

// buildColorspaceChain 返回 ffmpeg 使用的色彩空间转换过滤器链。
func buildColorspaceChain(info string, preferLibplacebo bool) string {
	switch {
	case shouldPreferLibplaceboColorspace(info) && preferLibplacebo:
		// Follow FFmpeg's documented CPU/llvmpipe example and keep the HDR/DV
		// libplacebo path conservative by disabling the expensive peak detector.
		return buildLibplaceboColorspaceChain(info)
	case strings.Contains(info, "bt2020") && (strings.Contains(info, "smpte2084") || strings.Contains(info, "arib-std-b67")):
		return "format=yuv420p10le,zscale=t=linear:npl=203,format=gbrpf32le,tonemap=mobius:param=0.3:desat=2.0,zscale=p=bt709:t=bt709:m=bt709,format=rgb24"
	case strings.Contains(info, "bt2020"):
		return "zscale=p=bt709:t=bt709:m=bt709,format=rgb24"
	default:
		return ""
	}
}

// shouldPreferLibplaceboColorspace 会判断当前色彩元数据是否更适合走 libplacebo 链路。
func shouldPreferLibplaceboColorspace(info string) bool {
	if strings.TrimSpace(info) == "" {
		return false
	}
	if strings.Contains(info, "dolby_vision=1") {
		return true
	}
	return strings.Contains(info, "bt2020") && (strings.Contains(info, "smpte2084") || strings.Contains(info, "arib-std-b67"))
}

// buildLibplaceboColorspaceChain 会构建 HDR/DV 转换到 sRGB 输出的 libplacebo 过滤器链。
func buildLibplaceboColorspaceChain(info string) string {
	// FFmpeg documents RGB output colorspace as gbr (AVCOL_SPC_RGB / sRGB).
	options := []string{
		"upscaler=none",
		"downscaler=none",
		"colorspace=gbr",
		"color_primaries=bt709",
		"color_trc=iec61966-2-1",
		"range=pc",
		"format=rgb24",
	}
	if strings.Contains(info, "dolby_vision=1") {
		options = append(options, "apply_dolbyvision=true")
	}
	options = append(options, "peak_detect=false")
	return "libplacebo=" + strings.Join(options, ":")
}

// parseColorspaceProbeJSON 会解析 ffprobe JSON 输出中的色彩空间和杜比视界元数据。
func parseColorspaceProbeJSON(stdout string) string {
	type ffprobeColorSideData struct {
		SideDataType string `json:"side_data_type"`
		DVProfile    int    `json:"dv_profile"`
	}
	type ffprobeColorStream struct {
		ColorSpace     string                 `json:"color_space"`
		ColorPrimaries string                 `json:"color_primaries"`
		ColorTransfer  string                 `json:"color_transfer"`
		SideDataList   []ffprobeColorSideData `json:"side_data_list"`
	}
	var payload struct {
		Streams []ffprobeColorStream `json:"streams"`
	}

	if strings.TrimSpace(stdout) == "" {
		return ""
	}
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		return ""
	}
	if len(payload.Streams) == 0 {
		return ""
	}

	stream := payload.Streams[0]
	lines := make([]string, 0, 5)
	if strings.TrimSpace(stream.ColorSpace) != "" {
		lines = append(lines, "color_space="+strings.TrimSpace(stream.ColorSpace))
	}
	if strings.TrimSpace(stream.ColorPrimaries) != "" {
		lines = append(lines, "color_primaries="+strings.TrimSpace(stream.ColorPrimaries))
	}
	if strings.TrimSpace(stream.ColorTransfer) != "" {
		lines = append(lines, "color_transfer="+strings.TrimSpace(stream.ColorTransfer))
	}
	for _, sideData := range stream.SideDataList {
		lowerType := strings.ToLower(strings.TrimSpace(sideData.SideDataType))
		if lowerType == "" {
			continue
		}
		if strings.Contains(lowerType, "dovi") || strings.Contains(lowerType, "dolby vision") {
			lines = append(lines, "dolby_vision=1")
			if sideData.DVProfile > 0 {
				lines = append(lines, fmt.Sprintf("dv_profile=%d", sideData.DVProfile))
			}
			break
		}
	}
	sort.Strings(lines)
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "|") + "|"
}

// buildDisplayAspectFilter 会构建显示参数显示比例过滤器，为后续流程准备好可直接使用的结果。
func buildDisplayAspectFilter() string {
	// DVD/VOB and other anamorphic sources often use non-square pixels.
	// Still-image formats do not reliably preserve SAR, so we expand to
	// square pixels before writing PNG/JPG.
	return "scale='trunc(iw*sar/2)*2:ih',setsar=1"
}

// detectDisplayGeometry 读取视频流的 SAR/DAR 元数据，并构建更适合静态截图的比例修正链和输出尺寸。
func (r *screenshotRunner) detectDisplayGeometry() (string, int, int) {
	stdout, _, err := system.RunCommand(r.ctx, r.tools.FFprobeBin,
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=width,height,sample_aspect_ratio,display_aspect_ratio",
		"-of", "default=noprint_wrappers=1",
		r.sourcePath,
	)
	if err != nil {
		return buildDisplayAspectFilter(), r.media.VideoWidth, r.media.VideoHeight
	}

	width := 0
	height := 0
	sar := ""
	dar := ""
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "width="):
			width, _ = strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "width=")))
		case strings.HasPrefix(line, "height="):
			height, _ = strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "height=")))
		case strings.HasPrefix(line, "sample_aspect_ratio="):
			sar = strings.TrimSpace(strings.TrimPrefix(line, "sample_aspect_ratio="))
		case strings.HasPrefix(line, "display_aspect_ratio="):
			dar = strings.TrimSpace(strings.TrimPrefix(line, "display_aspect_ratio="))
		}
	}

	if screenshotsource.LooksLikeDVDSource(r.sourcePath) {
		mediainfoDAR := ""
		if r.subtitleState.HasDVDMediaInfoResult {
			mediainfoDAR = strings.TrimSpace(r.subtitleState.DVDMediaInfoResult.DisplayAspectRatio)
		}
		r.logf("[信息] DVD 比例探测：ffprobe width=%d height=%d sar=%s dar=%s | mediainfo dar=%s",
			width,
			height,
			screenshottimestamps.DisplayProbeValue(sar),
			screenshottimestamps.DisplayProbeValue(dar),
			screenshottimestamps.DisplayProbeValue(mediainfoDAR),
		)
		if width2, height2, filter, ok := r.detectDVDDisplayAspectFilterFromMediaInfo(width, height); ok {
			r.logf("[信息] DVD 比例修正将直接使用 mediainfo DAR：%s", filter)
			displayWidth, displayHeight := detectDisplayDimensionsForMetadata(width2, height2, "", filter)
			return buildDisplayAspectFilterForMetadata(width2, height2, "", filter), displayWidth, displayHeight
		}
		r.logf("[提示] mediainfo 未提供可用 DVD 比例，回退 ffprobe SAR/DAR。")
	}

	displayWidth, displayHeight := detectDisplayDimensionsForMetadata(width, height, sar, dar)
	return buildDisplayAspectFilterForMetadata(width, height, sar, dar), displayWidth, displayHeight
}

// detectDVDDisplayAspectFilterFromMediaInfo 会从 mediainfo 结果中提取 DVD 比例修正所需参数。
func (r *screenshotRunner) detectDVDDisplayAspectFilterFromMediaInfo(width, height int) (int, int, string, bool) {
	if r == nil || !r.subtitleState.HasDVDMediaInfoResult {
		return 0, 0, "", false
	}
	if !screenshotsource.LooksLikeDVDSource(r.sourcePath) {
		return 0, 0, "", false
	}
	if width <= 0 || height <= 0 {
		return 0, 0, "", false
	}
	if strings.TrimSpace(r.subtitleState.DVDMediaInfoResult.DisplayAspectRatio) == "" {
		return 0, 0, "", false
	}
	return width, height, r.subtitleState.DVDMediaInfoResult.DisplayAspectRatio, true
}

// buildDisplayAspectFilterForMetadata 根据视频宽高和宽高比元数据生成静态截图所需的比例修正链。
func buildDisplayAspectFilterForMetadata(width, height int, sar, dar string) string {
	if width > 0 && height > 0 {
		normalizedDAR := screenshotdvdinfo.NormalizeAspectRatio(dar)
		if darNum, darDen, ok := parseAspectRatio(normalizedDAR); ok {
			rawAspect := float64(width) / float64(height)
			displayAspect := float64(darNum) / float64(darDen)
			if math.Abs(displayAspect-rawAspect) > 0.02 {
				return fmt.Sprintf("scale='trunc(ih*%d/%d/2)*2:ih',setsar=1", darNum, darDen)
			}
		} else if displayAspect, ok := parseAspectRatioValue(normalizedDAR); ok {
			rawAspect := float64(width) / float64(height)
			if math.Abs(displayAspect-rawAspect) > 0.02 {
				return fmt.Sprintf("scale='trunc(ih*%.6f/2)*2:ih',setsar=1", displayAspect)
			}
		}
	}

	if sarNum, sarDen, ok := parseAspectRatio(sar); ok {
		if sarNum == sarDen {
			return "setsar=1"
		}
	}
	return buildDisplayAspectFilter()
}

// detectDisplayDimensionsForMetadata 根据视频宽高和比例元数据估算最终截图尺寸。
func detectDisplayDimensionsForMetadata(width, height int, sar, dar string) (int, int) {
	if width <= 0 || height <= 0 {
		return 0, 0
	}

	normalizedDAR := screenshotdvdinfo.NormalizeAspectRatio(dar)
	if darNum, darDen, ok := parseAspectRatio(normalizedDAR); ok {
		rawAspect := float64(width) / float64(height)
		displayAspect := float64(darNum) / float64(darDen)
		if math.Abs(displayAspect-rawAspect) > 0.02 {
			return evenFloorDimension(float64(height) * displayAspect), height
		}
	} else if displayAspect, ok := parseAspectRatioValue(normalizedDAR); ok {
		rawAspect := float64(width) / float64(height)
		if math.Abs(displayAspect-rawAspect) > 0.02 {
			return evenFloorDimension(float64(height) * displayAspect), height
		}
	}

	if sarNum, sarDen, ok := parseAspectRatio(sar); ok {
		if sarNum == sarDen {
			return width, height
		}
		return evenFloorDimension(float64(width) * float64(sarNum) / float64(sarDen)), height
	}

	return width, height
}

// evenFloorDimension 会把尺寸向下规整为正偶数值。
func evenFloorDimension(value float64) int {
	if value <= 0 {
		return 0
	}
	size := int(math.Floor(value))
	if size <= 0 {
		return 0
	}
	if size%2 != 0 {
		size--
	}
	if size <= 0 {
		size = 2
	}
	return size
}

// parseAspectRatio 会把类似 16:9 的宽高比文本解析为整数分子分母。
func parseAspectRatio(raw string) (int, int, bool) {
	value := strings.TrimSpace(raw)
	if value == "" || value == "N/A" || value == "0:1" {
		return 0, 0, false
	}

	parts := strings.Split(value, ":")
	if len(parts) != 2 {
		return 0, 0, false
	}

	num, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
	den, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err1 != nil || err2 != nil || num <= 0 || den <= 0 {
		return 0, 0, false
	}
	return num, den, true
}

// parseAspectRatioValue 会把宽高比文本解析成浮点值，并兼容 16:9 与 1.778 两种写法。
func parseAspectRatioValue(raw string) (float64, bool) {
	if num, den, ok := parseAspectRatio(raw); ok {
		return float64(num) / float64(den), true
	}

	value := strings.TrimSpace(strings.ReplaceAll(raw, ",", "."))
	if value == "" || value == "N/A" {
		return 0, false
	}

	ratio, err := strconv.ParseFloat(value, 64)
	if err != nil || ratio <= 0 {
		return 0, false
	}
	return ratio, true
}

// joinFilters 连接多个非空 ffmpeg 过滤器片段。
func joinFilters(parts ...string) string {
	filters := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		filters = append(filters, part)
	}
	return strings.Join(filters, ",")
}

func firstFloatLine(output string) (float64, bool) {
	for _, line := range strings.Split(output, "\n") {
		value := strings.TrimSpace(line)
		if value == "" || value == "N/A" {
			continue
		}
		parsed, err := strconv.ParseFloat(value, 64)
		if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
			continue
		}
		return parsed, true
	}
	return 0, false
}
