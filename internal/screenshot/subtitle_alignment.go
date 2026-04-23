// Package screenshot 实现字幕时间对齐和字幕索引探测逻辑。

package screenshot

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"minfo/internal/system"
)

// resolveUniqueScreenshotSecond 会在已占用秒级时间点集合中寻找一个不冲突的截图时间点。
func (r *screenshotRunner) resolveUniqueScreenshotSecond(requested, aligned float64, usedSeconds map[int]struct{}) (float64, bool, bool) {
	aligned = r.clampToDuration(aligned)
	second := screenshotSecond(aligned)
	if _, exists := usedSeconds[second]; !exists {
		return aligned, false, true
	}

	if r.subtitle.Mode != "none" {
		r.ensureSubtitleIndex()
		for _, candidate := range r.uniqueAlignedCandidatesFromSubtitleIndex(requested) {
			candidate = r.clampToDuration(candidate)
			if _, exists := usedSeconds[screenshotSecond(candidate)]; exists {
				continue
			}
			return candidate, true, true
		}
	}

	return 0, false, false
}

// uniqueAlignedCandidatesFromSubtitleIndex 会根据字幕索引生成可去重的候选截图时间点。
func (r *screenshotRunner) uniqueAlignedCandidatesFromSubtitleIndex(requested float64) []float64 {
	if len(r.ensureSubtitleIndex()) == 0 {
		return nil
	}

	type secondCandidate struct {
		value    float64
		distance float64
		second   int
	}

	candidates := make([]secondCandidate, 0, len(r.subtitleIndex))
	seen := make(map[int]struct{}, len(r.subtitleIndex))
	for _, span := range r.subtitleIndex {
		startSecond := screenshotSecond(span.Start)
		endSecond := screenshotSecond(span.End)
		for second := startSecond; second <= endSecond; second++ {
			secondStart := math.Max(span.Start, float64(second))
			secondEnd := math.Min(span.End, float64(second)+0.999)
			if secondEnd < secondStart {
				continue
			}
			candidate := secondStart + (secondEnd-secondStart)/2
			secondKey := screenshotSecond(candidate)
			if _, exists := seen[secondKey]; exists {
				continue
			}
			seen[secondKey] = struct{}{}
			candidates = append(candidates, secondCandidate{
				value:    candidate,
				distance: math.Abs(candidate - requested),
				second:   secondKey,
			})
		}
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].distance == candidates[j].distance {
			if candidates[i].second == candidates[j].second {
				return candidates[i].value < candidates[j].value
			}
			return candidates[i].second < candidates[j].second
		}
		return candidates[i].distance < candidates[j].distance
	})

	values := make([]float64, 0, len(candidates))
	for _, candidate := range candidates {
		values = append(values, candidate.value)
	}
	return values
}

// detectStartOffset 读取当前截图源的起始时间偏移。
func (r *screenshotRunner) detectStartOffset() float64 {
	return r.detectStartOffsetForInput(r.sourcePath)
}

// detectStartOffsetForInput 读取指定输入的起始时间偏移。
func (r *screenshotRunner) detectStartOffsetForInput(input string) float64 {
	stdout, _, err := system.RunCommand(r.ctx, r.ffprobeBin,
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

	stdout, _, err = system.RunCommand(r.ctx, r.ffprobeBin,
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
	stdout, _, err := system.RunCommand(r.ctx, r.ffprobeBin,
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

	stdout, _, err := system.RunCommand(r.ctx, r.ffprobeBin,
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
	stdout, _, err := system.RunCommand(r.ctx, r.ffprobeBin,
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

	stdout, _, err = system.RunCommand(r.ctx, r.ffprobeBin,
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
	stdout, _, err := system.RunCommand(r.ctx, r.ffprobeBin,
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=width,height,sample_aspect_ratio,display_aspect_ratio",
		"-of", "default=noprint_wrappers=1",
		r.sourcePath,
	)
	if err != nil {
		return buildDisplayAspectFilter(), r.videoWidth, r.videoHeight
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

	if looksLikeDVDSource(r.dvdProbeSource()) {
		mediainfoDAR := ""
		if r.hasDVDMediaInfoResult {
			mediainfoDAR = strings.TrimSpace(r.dvdMediaInfoResult.DisplayAspectRatio)
		}
		r.logf("[信息] DVD 比例探测：ffprobe width=%d height=%d sar=%s dar=%s | mediainfo dar=%s",
			width,
			height,
			displayProbeValue(sar),
			displayProbeValue(dar),
			displayProbeValue(mediainfoDAR),
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
	if r == nil || !r.hasDVDMediaInfoResult {
		return 0, 0, "", false
	}
	if !looksLikeDVDSource(r.dvdProbeSource()) {
		return 0, 0, "", false
	}
	if width <= 0 || height <= 0 {
		return 0, 0, "", false
	}
	if strings.TrimSpace(r.dvdMediaInfoResult.DisplayAspectRatio) == "" {
		return 0, 0, "", false
	}
	return width, height, r.dvdMediaInfoResult.DisplayAspectRatio, true
}

// buildDisplayAspectFilterForMetadata 根据视频宽高和宽高比元数据生成静态截图所需的比例修正链。
func buildDisplayAspectFilterForMetadata(width, height int, sar, dar string) string {
	if width > 0 && height > 0 {
		normalizedDAR := normalizeMediaInfoAspectRatio(dar)
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

	normalizedDAR := normalizeMediaInfoAspectRatio(dar)
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

// pgsBitmapPacketMinSize 返回探测 PGS 位图字幕区间时使用的最小包大小阈值。
func pgsBitmapPacketMinSize() int {
	return 1500
}

// dvdBitmapPacketMinSize 返回探测 DVD 位图字幕区间时使用的最小包大小阈值。
func dvdBitmapPacketMinSize() int {
	return 1
}

// alignToSubtitle 会基于全片字幕索引选择最终截图时间点。
func (r *screenshotRunner) alignToSubtitle(requested float64) float64 {
	if r.subtitle.Mode == "none" {
		return requested
	}

	index := r.ensureSubtitleIndex()
	if len(index) == 0 {
		r.logf("[提示] 全片字幕索引未找到可用字幕事件，按原时间点截图：%s", secToHMSMS(requested))
		return requested
	}

	if r.subtitle.Mode == "internal" && r.isSupportedBitmapSubtitle() {
		r.logBitmapSubtitleVisibilityProgress()
		if candidate, ok := r.findNearestVisibleBitmapIndexedCandidate(requested); ok {
			return r.logAlignedSubtitleIndexCandidate(requested, candidate)
		}
		r.logf("[提示] 全片字幕索引未找到可见字幕事件，按原时间点截图：%s", secToHMSMS(requested))
		return requested
	}

	if candidate, ok := snapFromIndex(requested, index, subtitleSnapEpsilon); ok {
		candidate = r.clampToDuration(candidate)
		return r.logAlignedSubtitleIndexCandidate(requested, candidate)
	}

	r.logf("[提示] 全片字幕索引未找到可用字幕事件，按原时间点截图：%s", secToHMSMS(requested))
	return requested
}

// logAlignedSubtitleIndexCandidate 会记录全片字幕索引命中的对齐结果并返回最终时间点。
func (r *screenshotRunner) logAlignedSubtitleIndexCandidate(requested, candidate float64) float64 {
	candidate = r.clampToDuration(candidate)
	if floatDiffGT(candidate, requested) {
		r.logf("[对齐] 请求 %s → 全片字幕索引 %s", secToHMSMS(requested), secToHMSMS(candidate))
	} else {
		r.logf("[提示] 已直接复用全片字幕索引对齐到原时间点附近：%s", secToHMSMS(candidate))
	}
	return candidate
}

// acceptBitmapSubtitleCandidate 在接受位图候选时间点前验证该时刻是否真的渲染出字幕。
func (r *screenshotRunner) acceptBitmapSubtitleCandidate(label string, candidate float64) (float64, bool) {
	candidate = r.clampToDuration(candidate)
	key := bitmapCandidateKey(candidate)
	if _, rejected := r.rejectedBitmapCandidates[key]; rejected {
		return 0, false
	}

	visible, err := r.bitmapSubtitleVisibleAt(candidate)
	if err != nil {
		r.logf("[提示] %s候选可视性验证失败，沿用该时间点：%s | 原因：%s",
			label,
			secToHMSMS(candidate),
			err.Error(),
		)
		return candidate, true
	}
	if !visible {
		if r != nil && r.subtitle.Mode == "internal" && r.isSupportedBitmapSubtitle() {
			shortBack := r.renderCoarseBack()
			longBack := r.settings.CoarseBackPGS
			if longBack > shortBack {
				longVisible, longErr := r.internalBitmapSubtitleVisibleAtWithCoarseBack(candidate, longBack)
				if longErr == nil && longVisible {
					r.bitmapRenderBackOverride = longBack
					r.logf("[提示] %s候选仅在较大回溯窗口下渲染出字幕，后续位图截图改用 %ds 回溯窗口：%s",
						label,
						longBack,
						secToHMSMS(candidate),
					)
					return candidate, true
				}
			}
		}
		if r.rejectedBitmapCandidates == nil {
			r.rejectedBitmapCandidates = make(map[string]struct{})
		}
		r.rejectedBitmapCandidates[key] = struct{}{}
		r.logf("[提示] %s候选未实际渲染出字幕，继续搜索：%s",
			label,
			secToHMSMS(candidate),
		)
		return 0, false
	}
	return candidate, true
}

// findNearestVisibleBitmapIndexedCandidate 会查找最近可见的全片位图字幕索引候选项。
func (r *screenshotRunner) findNearestVisibleBitmapIndexedCandidate(requested float64) (float64, bool) {
	if len(r.ensureSubtitleIndex()) == 0 {
		return 0, false
	}

	spans := append([]subtitleSpan(nil), r.subtitleIndex...)
	sort.Slice(spans, func(i, j int) bool {
		left := math.Abs(bitmapSnapPoint(spans[i], subtitleSnapEpsilon) - requested)
		right := math.Abs(bitmapSnapPoint(spans[j], subtitleSnapEpsilon) - requested)
		if left == right {
			return spans[i].Start < spans[j].Start
		}
		return left < right
	})

	limit := len(spans)
	if limit > 8 {
		limit = 8
	}
	for _, span := range spans[:limit] {
		candidate, ok := r.acceptBitmapSubtitleCandidate("全片字幕索引", bitmapSnapPoint(span, subtitleSnapEpsilon))
		if ok {
			return candidate, true
		}
	}
	return 0, false
}

// buildSubtitleIndex 会扫描当前字幕源并建立可复用的全片字幕索引。
func (r *screenshotRunner) buildSubtitleIndex() []subtitleSpan {
	if r.subtitle.Mode == "none" {
		return nil
	}

	var spans []subtitleSpan
	var err error

	if r.subtitle.Mode == "internal" && r.isSupportedBitmapSubtitle() {
		spans, err = r.probeSupportedBitmapSpans(-1, 0)
	} else if r.subtitle.Mode == "internal" {
		spans, err = r.probeInternalTextSpans(-1, 0)
	} else {
		spans, err = r.probeExternalTextSpans(-1, 0)
	}
	if err != nil {
		r.logf("[提示] 全片字幕索引构建失败：%s", err.Error())
		return nil
	}
	if len(spans) == 0 {
		r.logf("[提示] 全片字幕索引未发现可用字幕事件。")
		return nil
	}

	if r.subtitle.Mode == "internal" && r.isSupportedBitmapSubtitle() {
		if r.isDVDSubtitle() {
			spans = mergeNearbySubtitleSpans(spans, 0.75)
			r.logf("[信息] 全片字幕索引已建立（DVD 位图字幕，共 %d 段）。", len(spans))
			return spans
		}
		r.logf("[信息] 全片字幕索引已建立（PGS 位图字幕，共 %d 段）。", len(spans))
		return spans
	}

	r.logf("[信息] 全片字幕索引已建立（文字字幕，共 %d 段）。", len(spans))
	return spans
}

// shouldEmitSubtitleIndexProgress 会判断扫描全片字幕索引时是否需要对外发送进度。
func (r *screenshotRunner) shouldEmitSubtitleIndexProgress() bool {
	if r == nil || r.subtitle.Mode == "none" {
		return false
	}
	return !r.subtitle.ExtractedText
}

// subtitleIndexProgressDetail 会返回全片字幕索引阶段适合展示的进度详情文案。
func (r *screenshotRunner) subtitleIndexProgressDetail() string {
	if r == nil {
		return "正在扫描全片字幕索引。"
	}
	switch {
	case r.subtitle.Mode == "internal" && r.isPGSSubtitle():
		return "正在扫描全片 PGS 字幕索引。"
	case r.subtitle.Mode == "internal" && r.isDVDSubtitle():
		return "正在扫描全片 DVD 字幕索引。"
	case r.subtitle.Mode == "external":
		return "正在扫描全片外挂字幕索引。"
	default:
		return "正在扫描全片字幕索引。"
	}
}

// ensureSubtitleIndex 会按需建立并缓存全片字幕索引，同时负责索引阶段进度日志。
func (r *screenshotRunner) ensureSubtitleIndex() []subtitleSpan {
	if r == nil {
		return nil
	}
	if r.subtitleIndexBuilt {
		return r.subtitleIndex
	}

	stopHeartbeat := func() {}
	if r.shouldEmitSubtitleIndexProgress() {
		detail := r.subtitleIndexProgressDetail()
		r.logProgress("字幕", 3, 3, detail)
		if !r.canApproximateSubtitleIndexScanProgress() {
			stopHeartbeat = r.startProgressHeartbeat("字幕", detail)
		}
	}

	r.subtitleIndex = r.buildSubtitleIndex()
	stopHeartbeat()
	if r.shouldEmitSubtitleIndexProgress() {
		r.logProgressPercent("字幕", 100, "全片字幕索引准备完成。")
	}
	r.subtitleIndexBuilt = true
	return r.subtitleIndex
}

// probeSupportedBitmapSpans 根据字幕类型分派到对应的位图字幕区间探测逻辑。
func (r *screenshotRunner) probeSupportedBitmapSpans(startAbs, duration float64) ([]subtitleSpan, error) {
	switch {
	case r.isPGSSubtitle():
		return r.probePGSSubtitleSpans(startAbs, duration)
	case r.isDVDSubtitle():
		return r.probeDVDSubtitleSpans(startAbs, duration)
	default:
		return nil, nil
	}
}

// probePGSSubtitleSpans 探测 PGS 字幕的时间区间。
func (r *screenshotRunner) probePGSSubtitleSpans(startAbs, duration float64) ([]subtitleSpan, error) {
	return r.probeInternalBitmapSpans(startAbs, duration, pgsBitmapPacketMinSize())
}

// probeDVDSubtitleSpans 探测 DVD 字幕的时间区间。
func (r *screenshotRunner) probeDVDSubtitleSpans(startAbs, duration float64) ([]subtitleSpan, error) {
	return r.probeInternalBitmapSpans(startAbs, duration, dvdBitmapPacketMinSize())
}

// probeInternalBitmapSpans 用 ffprobe 包信息构建内封位图字幕的时间区间。
func (r *screenshotRunner) probeInternalBitmapSpans(startAbs, duration float64, bitmapMinSize int) ([]subtitleSpan, error) {
	args := []string{
		"-probesize", r.settings.ProbeSize,
		"-analyzeduration", r.settings.Analyze,
		"-v", "error",
		"-select_streams", fmt.Sprintf("s:%d", r.subtitle.RelativeIndex),
	}
	if startAbs >= 0 {
		args = append(args, "-read_intervals", readInterval(startAbs, duration))
	}
	args = append(args,
		"-show_packets",
		"-show_entries", "packet=pts_time,duration_time,size",
		"-of", "compact=print_section=0:nokey=1:escape=none",
		r.sourcePath,
	)
	return r.probePacketSpans(args, true, bitmapMinSize, startAbs, duration)
}

// probeInternalTextSpans 用 ffprobe 包信息构建内封文字字幕的时间区间。
func (r *screenshotRunner) probeInternalTextSpans(startAbs, duration float64) ([]subtitleSpan, error) {
	args := []string{
		"-probesize", r.settings.ProbeSize,
		"-analyzeduration", r.settings.Analyze,
		"-v", "error",
		"-select_streams", fmt.Sprintf("s:%d", r.subtitle.RelativeIndex),
	}
	if startAbs >= 0 {
		args = append(args, "-read_intervals", readInterval(startAbs, duration))
	}
	args = append(args,
		"-show_packets",
		"-show_entries", "packet=pts_time,duration_time",
		"-of", "compact=print_section=0:nokey=1:escape=none",
		r.sourcePath,
	)
	return r.probePacketSpans(args, true, -1, startAbs, duration)
}

// probeExternalTextSpans 用 ffprobe 包信息构建外挂文字字幕的时间区间。
func (r *screenshotRunner) probeExternalTextSpans(start, duration float64) ([]subtitleSpan, error) {
	args := []string{"-v", "error"}
	if start >= 0 {
		args = append(args, "-read_intervals", readInterval(start, duration))
	}
	args = append(args,
		"-show_packets",
		"-show_entries", "packet=pts_time,duration_time",
		"-of", "compact=print_section=0:nokey=1:escape=none",
		r.subtitle.File,
	)
	return r.probePacketSpans(args, false, -1, start, duration)
}

// probePacketSpans 把 ffprobe 返回的包数据转换成排序后的字幕区间列表。
func (r *screenshotRunner) probePacketSpans(args []string, internal bool, bitmapMinSize int, startAbs, duration float64) ([]subtitleSpan, error) {
	spans := make([]subtitleSpan, 0, 256)
	progress := newSubtitleIndexProgressEmitter(r, internal, startAbs, duration)

	stdout, stderr, err := system.RunCommandLive(r.ctx, r.ffprobeBin, func(stream, line string) {
		if stream != "stdout" {
			return
		}
		packet, ok := parseFFprobePacketCompactLine(line)
		if !ok {
			return
		}
		progress.observe(packet)
		spans = appendPacketSpan(spans, packet, internal, bitmapMinSize, r.startOffset)
	}, args...)
	if err != nil {
		return nil, fmt.Errorf(system.BestErrorMessage(err, stderr, stdout))
	}
	if strings.TrimSpace(stdout) == "" {
		return nil, nil
	}

	sort.Slice(spans, func(i, j int) bool {
		if spans[i].Start == spans[j].Start {
			return spans[i].End < spans[j].End
		}
		return spans[i].Start < spans[j].Start
	})
	if bitmapMinSize >= 0 {
		return mergeNearbySubtitleSpans(spans, 0.75), nil
	}
	return spans, nil
}

// appendPacketSpan 会把单条 ffprobe 包记录转换为字幕区间并追加到结果集中。
func appendPacketSpan(spans []subtitleSpan, packet ffprobePacket, internal bool, bitmapMinSize int, startOffset float64) []subtitleSpan {
	pts, ok := parseFloatString(packet.PTSTime)
	if !ok {
		return spans
	}
	durationValue, ok := parseFloatString(packet.DurationTime)
	if !ok {
		durationValue = defaultSubtitleDuration
	}
	if bitmapMinSize >= 0 {
		sizeValue, ok := parseIntString(packet.Size)
		if !ok || sizeValue < bitmapMinSize {
			return spans
		}
	}

	start := pts
	end := pts + durationValue
	if internal {
		start -= startOffset
		end -= startOffset
	}
	if end < 0 {
		return spans
	}
	if start < 0 {
		start = 0
	}
	return append(spans, subtitleSpan{Start: start, End: end})
}

// canApproximateSubtitleIndexScanProgress 会判断当前是否具备按时长估算索引进度的条件。
func (r *screenshotRunner) canApproximateSubtitleIndexScanProgress() bool {
	return r != nil && r.duration > 0
}

type subtitleIndexProgressEmitter struct {
	runner       *screenshotRunner
	baseDetail   string
	scanStart    float64
	scanTotal    float64
	lastPercent  float64
	lastScanTime float64
	lastEmitAt   time.Time
	maxPTS       float64
	enabled      bool
}

// newSubtitleIndexProgressEmitter 会为全片字幕索引扫描创建基于 pts 的进度发射器。
func newSubtitleIndexProgressEmitter(r *screenshotRunner, internal bool, startAbs, duration float64) *subtitleIndexProgressEmitter {
	emitter := &subtitleIndexProgressEmitter{
		runner:       r,
		lastScanTime: -1,
	}
	if r == nil || !r.shouldEmitSubtitleIndexProgress() {
		return emitter
	}
	if startAbs >= 0 || duration > 0 || r.subtitleIndexBuilt {
		return emitter
	}
	if !r.canApproximateSubtitleIndexScanProgress() {
		return emitter
	}

	scanStart := 0.0
	if internal {
		scanStart = math.Max(r.startOffset, 0)
	}
	emitter.baseDetail = r.subtitleIndexProgressDetail()
	emitter.scanStart = scanStart
	emitter.scanTotal = r.duration
	emitter.maxPTS = scanStart
	emitter.enabled = emitter.scanTotal > 0
	return emitter
}

// observe 会根据当前读取到的字幕包更新时间和进度日志。
func (e *subtitleIndexProgressEmitter) observe(packet ffprobePacket) {
	if e == nil || !e.enabled || e.runner == nil {
		return
	}
	pts, ok := parseFloatString(packet.PTSTime)
	if !ok {
		return
	}
	if pts > e.maxPTS {
		e.maxPTS = pts
	} else {
		pts = e.maxPTS
	}

	scanned := pts - e.scanStart
	if scanned < 0 {
		scanned = 0
	}
	if scanned > e.scanTotal {
		scanned = e.scanTotal
	}

	percent := subtitleIndexScanProgressPercent(scanned, e.scanTotal)
	if !e.shouldEmit(scanned, percent) {
		return
	}

	e.lastPercent = percent
	e.lastScanTime = scanned
	e.lastEmitAt = time.Now()
	e.runner.logProgressPercent("字幕", percent, subtitleIndexScanProgressDetail(e.baseDetail, scanned, e.scanTotal))
}

// shouldEmit 会判断本次扫描进度是否值得对外发送一条新日志。
func (e *subtitleIndexProgressEmitter) shouldEmit(scanned, percent float64) bool {
	if percent <= 0 {
		return false
	}
	if percent >= 94 && e.lastPercent >= 94 {
		return false
	}
	if e.lastPercent <= 0 {
		return true
	}
	if percent-e.lastPercent >= 1 {
		return true
	}
	if scanned-e.lastScanTime >= 15 {
		return true
	}
	if e.lastEmitAt.IsZero() {
		return true
	}
	return time.Since(e.lastEmitAt) >= time.Second && percent > e.lastPercent
}

// subtitleIndexScanProgressPercent 会把已扫描时长转换为索引阶段使用的百分比。
func subtitleIndexScanProgressPercent(scanned, total float64) float64 {
	if total <= 0 {
		return 0
	}
	ratio := scanned / total
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}
	percent := 94 * ratio
	if percent < 0.1 && scanned > 0 {
		percent = 0.1
	}
	return clampProgressPercent(minFloat(percent, 94))
}

// subtitleIndexScanProgressDetail 会把索引阶段基础文案和扫描进度拼接成展示文本。
func subtitleIndexScanProgressDetail(detail string, scanned, total float64) string {
	base := strings.TrimSpace(detail)
	if base == "" {
		base = "正在扫描全片字幕索引。"
	}
	if total <= 0 {
		return base
	}
	if scanned < 0 {
		scanned = 0
	}
	if scanned > total {
		scanned = total
	}
	return fmt.Sprintf("%s | 已扫描 %s / %s", base, secToHMS(scanned), secToHMS(total))
}

// parseFFprobePacketCompactLine 会解析 ffprobe compact 输出中的单行 packet 数据。
func parseFFprobePacketCompactLine(line string) (ffprobePacket, bool) {
	text := strings.TrimSpace(line)
	if text == "" {
		return ffprobePacket{}, false
	}

	fields := strings.Split(text, "|")
	if len(fields) == 0 {
		return ffprobePacket{}, false
	}
	if strings.EqualFold(strings.TrimSpace(fields[0]), "packet") {
		fields = fields[1:]
	}
	if len(fields) == 0 {
		return ffprobePacket{}, false
	}

	packet := ffprobePacket{}
	if !strings.Contains(fields[0], "=") {
		packet.PTSTime = strings.TrimSpace(fields[0])
		if len(fields) > 1 {
			packet.DurationTime = strings.TrimSpace(fields[1])
		}
		if len(fields) > 2 {
			packet.Size = strings.TrimSpace(fields[2])
		}
		return packet, true
	}

	for _, field := range fields {
		key, value, ok := strings.Cut(field, "=")
		if !ok {
			continue
		}
		switch strings.TrimSpace(key) {
		case "pts_time":
			packet.PTSTime = strings.TrimSpace(value)
		case "duration_time":
			packet.DurationTime = strings.TrimSpace(value)
		case "size":
			packet.Size = strings.TrimSpace(value)
		}
	}

	if packet.PTSTime == "" && packet.DurationTime == "" && packet.Size == "" {
		return ffprobePacket{}, false
	}
	return packet, true
}

// clampToDuration 把时间点限制在 [0, duration] 范围内。
func (r *screenshotRunner) clampToDuration(value float64) float64 {
	if value < 0 {
		return 0
	}
	if r.duration > 0 && value > r.duration {
		return r.duration
	}
	return value
}
