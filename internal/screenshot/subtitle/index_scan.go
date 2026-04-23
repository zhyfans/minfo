// Package subtitle 提供全片字幕索引扫描、进度估算与 ffprobe 包解析能力。

package subtitle

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	screenshotprogress "minfo/internal/screenshot/progress"
	screenshotruntime "minfo/internal/screenshot/runtime"
	"minfo/internal/screenshot/timestamps"
	"minfo/internal/system"
)

const defaultSubtitleDuration = 4.0

type indexProgressEmitter struct {
	runner       *Runner
	baseDetail   string
	scanStart    float64
	scanTotal    float64
	lastPercent  float64
	lastScanTime float64
	lastEmitAt   time.Time
	maxPTS       float64
	enabled      bool
}

func pgsBitmapPacketMinSize() int { return 1500 }

func dvdBitmapPacketMinSize() int { return 1 }

func (r *Runner) buildIndex() []screenshotruntime.SubtitleSpan {
	selection := r.selection()
	if selection.Mode == "none" {
		return nil
	}

	var spans []screenshotruntime.SubtitleSpan
	var err error

	if selection.Mode == "internal" && r.isSupportedBitmapSubtitle() {
		spans, err = r.probeSupportedBitmapSpans(-1, 0)
	} else if selection.Mode == "internal" {
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

	if selection.Mode == "internal" && r.isSupportedBitmapSubtitle() {
		if r.isDVDSubtitle() {
			spans = MergeNearbySpans(spans, 0.75)
			r.logf("[信息] 全片字幕索引已建立（DVD 位图字幕，共 %d 段）。", len(spans))
			return spans
		}
		r.logf("[信息] 全片字幕索引已建立（PGS 位图字幕，共 %d 段）。", len(spans))
		return spans
	}

	r.logf("[信息] 全片字幕索引已建立（文字字幕，共 %d 段）。", len(spans))
	return spans
}

// ShouldEmitIndexProgress 会判断扫描全片字幕索引时是否需要对外发送进度。
func (r *Runner) ShouldEmitIndexProgress() bool {
	selection := r.selection()
	if selection.Mode == "none" {
		return false
	}
	return !selection.ExtractedText
}

func (r *Runner) canApproximateIndexScanProgress() bool {
	return r != nil && r.media().Duration > 0
}

func (r *Runner) indexProgressDetail() string {
	if r == nil {
		return "正在扫描全片字幕索引。"
	}
	switch {
	case r.selection().Mode == "internal" && r.isPGSSubtitle():
		return "正在扫描全片 PGS 字幕索引。"
	case r.selection().Mode == "internal" && r.isDVDSubtitle():
		return "正在扫描全片 DVD 字幕索引。"
	case r.selection().Mode == "external":
		return "正在扫描全片外挂字幕索引。"
	default:
		return "正在扫描全片字幕索引。"
	}
}

// EnsureIndex 会按需建立并缓存全片字幕索引，同时负责索引阶段进度日志。
func (r *Runner) EnsureIndex() []screenshotruntime.SubtitleSpan {
	if r == nil {
		return nil
	}
	state := r.state()
	if state.IndexBuilt {
		return state.Index
	}

	stopHeartbeat := func() {}
	if r.ShouldEmitIndexProgress() {
		detail := r.indexProgressDetail()
		r.logProgress("字幕", 3, 3, detail)
		if !r.canApproximateIndexScanProgress() {
			stopHeartbeat = r.startHeartbeat("字幕", detail)
		}
	}

	state.Index = r.buildIndex()
	stopHeartbeat()
	if r.ShouldEmitIndexProgress() {
		r.logProgressPercent("字幕", 100, "全片字幕索引准备完成。")
	}
	state.IndexBuilt = true
	return state.Index
}

func newIndexProgressEmitter(r *Runner, internal bool, startAbs, duration float64) *indexProgressEmitter {
	emitter := &indexProgressEmitter{runner: r, lastScanTime: -1}
	if r == nil || !r.ShouldEmitIndexProgress() {
		return emitter
	}
	if startAbs >= 0 || duration > 0 || r.state().IndexBuilt {
		return emitter
	}
	if !r.canApproximateIndexScanProgress() {
		return emitter
	}

	scanStart := 0.0
	if internal {
		scanStart = math.Max(r.media().StartOffset, 0)
	}
	emitter.baseDetail = r.indexProgressDetail()
	emitter.scanStart = scanStart
	emitter.scanTotal = r.media().Duration
	emitter.maxPTS = scanStart
	emitter.enabled = emitter.scanTotal > 0
	return emitter
}

func (e *indexProgressEmitter) observe(packet screenshotruntime.FFprobePacket) {
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

	percent := IndexScanProgressPercent(scanned, e.scanTotal)
	if !e.shouldEmit(scanned, percent) {
		return
	}

	e.lastPercent = percent
	e.lastScanTime = scanned
	e.lastEmitAt = time.Now()
	e.runner.logProgressPercent("字幕", percent, IndexScanProgressDetail(e.baseDetail, scanned, e.scanTotal))
}

func (e *indexProgressEmitter) shouldEmit(scanned, percent float64) bool {
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

func (r *Runner) probeSupportedBitmapSpans(startAbs, duration float64) ([]screenshotruntime.SubtitleSpan, error) {
	switch {
	case r.isPGSSubtitle():
		return r.probePGSSubtitleSpans(startAbs, duration)
	case r.isDVDSubtitle():
		return r.probeDVDSubtitleSpans(startAbs, duration)
	default:
		return nil, nil
	}
}

func (r *Runner) probePGSSubtitleSpans(startAbs, duration float64) ([]screenshotruntime.SubtitleSpan, error) {
	return r.probeInternalBitmapSpans(startAbs, duration, pgsBitmapPacketMinSize())
}

func (r *Runner) probeDVDSubtitleSpans(startAbs, duration float64) ([]screenshotruntime.SubtitleSpan, error) {
	return r.probeInternalBitmapSpans(startAbs, duration, dvdBitmapPacketMinSize())
}

func (r *Runner) probeInternalBitmapSpans(startAbs, duration float64, bitmapMinSize int) ([]screenshotruntime.SubtitleSpan, error) {
	args := []string{
		"-probesize", r.Settings.ProbeSize,
		"-analyzeduration", r.Settings.Analyze,
		"-v", "error",
		"-select_streams", fmt.Sprintf("s:%d", r.selection().RelativeIndex),
	}
	if startAbs >= 0 {
		args = append(args, "-read_intervals", timestamps.ReadInterval(startAbs, duration))
	}
	args = append(args,
		"-show_packets",
		"-show_entries", "packet=pts_time,duration_time,size",
		"-of", "compact=print_section=0:nokey=1:escape=none",
		r.SourcePath,
	)
	return r.ProbePacketSpans(args, true, bitmapMinSize, startAbs, duration)
}

func (r *Runner) probeInternalTextSpans(startAbs, duration float64) ([]screenshotruntime.SubtitleSpan, error) {
	args := []string{
		"-probesize", r.Settings.ProbeSize,
		"-analyzeduration", r.Settings.Analyze,
		"-v", "error",
		"-select_streams", fmt.Sprintf("s:%d", r.selection().RelativeIndex),
	}
	if startAbs >= 0 {
		args = append(args, "-read_intervals", timestamps.ReadInterval(startAbs, duration))
	}
	args = append(args,
		"-show_packets",
		"-show_entries", "packet=pts_time,duration_time",
		"-of", "compact=print_section=0:nokey=1:escape=none",
		r.SourcePath,
	)
	return r.ProbePacketSpans(args, true, -1, startAbs, duration)
}

func (r *Runner) probeExternalTextSpans(start, duration float64) ([]screenshotruntime.SubtitleSpan, error) {
	args := []string{"-v", "error"}
	if start >= 0 {
		args = append(args, "-read_intervals", timestamps.ReadInterval(start, duration))
	}
	args = append(args,
		"-show_packets",
		"-show_entries", "packet=pts_time,duration_time",
		"-of", "compact=print_section=0:nokey=1:escape=none",
		r.selection().File,
	)
	return r.ProbePacketSpans(args, false, -1, start, duration)
}

// ProbePacketSpans 会把 ffprobe 返回的包数据转换成排序后的字幕区间列表。
func (r *Runner) ProbePacketSpans(args []string, internal bool, bitmapMinSize int, startAbs, duration float64) ([]screenshotruntime.SubtitleSpan, error) {
	spans := make([]screenshotruntime.SubtitleSpan, 0, 256)
	progress := newIndexProgressEmitter(r, internal, startAbs, duration)

	stdout, stderr, err := system.RunCommandLive(r.Ctx, r.Tools.FFprobeBin, func(stream, line string) {
		if stream != "stdout" {
			return
		}
		packet, ok := ParseFFprobePacketCompactLine(line)
		if !ok {
			return
		}
		progress.observe(packet)
		spans = appendPacketSpan(spans, packet, internal, bitmapMinSize, r.media().StartOffset)
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
		return MergeNearbySpans(spans, 0.75), nil
	}
	return spans, nil
}

func appendPacketSpan(spans []screenshotruntime.SubtitleSpan, packet screenshotruntime.FFprobePacket, internal bool, bitmapMinSize int, startOffset float64) []screenshotruntime.SubtitleSpan {
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
	return append(spans, screenshotruntime.SubtitleSpan{Start: start, End: end})
}

// IndexScanProgressPercent 会把已扫描时长转换为索引阶段使用的百分比。
func IndexScanProgressPercent(scanned, total float64) float64 {
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
	return screenshotprogress.ClampPercent(minFloat(percent, 94))
}

// IndexScanProgressDetail 会把索引阶段基础文案和扫描进度拼接成展示文本。
func IndexScanProgressDetail(detail string, scanned, total float64) string {
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
	return fmt.Sprintf("%s | 已扫描 %s / %s", base, timestamps.SecToHMS(scanned), timestamps.SecToHMS(total))
}

// ParseFFprobePacketCompactLine 会解析 ffprobe compact 输出中的单行 packet 数据。
func ParseFFprobePacketCompactLine(line string) (screenshotruntime.FFprobePacket, bool) {
	text := strings.TrimSpace(line)
	if text == "" {
		return screenshotruntime.FFprobePacket{}, false
	}

	fields := strings.Split(text, "|")
	if len(fields) == 0 {
		return screenshotruntime.FFprobePacket{}, false
	}
	if strings.EqualFold(strings.TrimSpace(fields[0]), "packet") {
		fields = fields[1:]
	}
	if len(fields) == 0 {
		return screenshotruntime.FFprobePacket{}, false
	}

	packet := screenshotruntime.FFprobePacket{}
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
		return screenshotruntime.FFprobePacket{}, false
	}
	return packet, true
}

func parseFloatString(value string) (float64, bool) {
	text := strings.TrimSpace(value)
	if text == "" || text == "N/A" {
		return 0, false
	}
	parsed, err := strconv.ParseFloat(text, 64)
	if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
		return 0, false
	}
	return parsed, true
}

func parseIntString(value string) (int, bool) {
	text := strings.TrimSpace(value)
	if text == "" {
		return 0, false
	}
	parsed, err := strconv.Atoi(text)
	if err != nil {
		return 0, false
	}
	return parsed, true
}

func minFloat(left, right float64) float64 {
	if left < right {
		return left
	}
	return right
}
