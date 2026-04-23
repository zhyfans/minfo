// Package screenshot 实现全片字幕索引扫描与 ffprobe 包解析逻辑。

package screenshot

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"minfo/internal/system"
)

// pgsBitmapPacketMinSize 返回探测 PGS 位图字幕区间时使用的最小包大小阈值。
func pgsBitmapPacketMinSize() int {
	return 1500
}

// dvdBitmapPacketMinSize 返回探测 DVD 位图字幕区间时使用的最小包大小阈值。
func dvdBitmapPacketMinSize() int {
	return 1
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
	if r.subtitleState.IndexBuilt {
		return r.subtitleState.Index
	}

	stopHeartbeat := func() {}
	if r.shouldEmitSubtitleIndexProgress() {
		detail := r.subtitleIndexProgressDetail()
		r.logProgress("字幕", 3, 3, detail)
		if !r.canApproximateSubtitleIndexScanProgress() {
			stopHeartbeat = r.startProgressHeartbeat("字幕", detail)
		}
	}

	r.subtitleState.Index = r.buildSubtitleIndex()
	stopHeartbeat()
	if r.shouldEmitSubtitleIndexProgress() {
		r.logProgressPercent("字幕", 100, "全片字幕索引准备完成。")
	}
	r.subtitleState.IndexBuilt = true
	return r.subtitleState.Index
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

	stdout, stderr, err := system.RunCommandLive(r.ctx, r.tools.FFprobeBin, func(stream, line string) {
		if stream != "stdout" {
			return
		}
		packet, ok := parseFFprobePacketCompactLine(line)
		if !ok {
			return
		}
		progress.observe(packet)
		spans = appendPacketSpan(spans, packet, internal, bitmapMinSize, r.media.StartOffset)
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
	return r != nil && r.media.Duration > 0
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
	if startAbs >= 0 || duration > 0 || r.subtitleState.IndexBuilt {
		return emitter
	}
	if !r.canApproximateSubtitleIndexScanProgress() {
		return emitter
	}

	scanStart := 0.0
	if internal {
		scanStart = math.Max(r.media.StartOffset, 0)
	}
	emitter.baseDetail = r.subtitleIndexProgressDetail()
	emitter.scanStart = scanStart
	emitter.scanTotal = r.media.Duration
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
