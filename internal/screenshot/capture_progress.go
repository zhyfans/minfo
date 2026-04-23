// Package screenshot 负责截图流程中的实时进度估算与阶段日志文案。

package screenshot

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	screenshotprogress "minfo/internal/screenshot/progress"
	screenshottimestamps "minfo/internal/screenshot/timestamps"
)

type ffmpegRealtimeState struct {
	mu                sync.Mutex
	frame             string
	fps               string
	outTime           string
	outTimeMS         int64
	speed             string
	totalSize         string
	heartbeatCount    int
	lastLoggedPercent float64
	lastLoggedDetail  string
	startedAt         time.Time
	windowSeconds     float64
	firstOutTimeMS    int64
	hasFirstOutTime   bool
}

// consumeFFmpegProgressLine 会把单行 -progress 输出更新到实时进度状态。
func (r *screenshotRunner) consumeFFmpegProgressLine(line string, state *ffmpegRealtimeState, stage string, detailBuilder func(*ffmpegRealtimeState) string) {
	if line == "" {
		return
	}

	key, value, ok := strings.Cut(line, "=")
	if !ok {
		return
	}

	if key == "progress" {
		r.emitFFmpegRealtimeProgress(strings.TrimSpace(value), state, stage, detailBuilder)
		return
	}

	state.mu.Lock()
	switch key {
	case "frame":
		state.frame = value
	case "fps":
		state.fps = value
	case "out_time":
		state.outTime = value
	case "out_time_ms":
		if parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64); err == nil {
			state.outTimeMS = parsed
			if !state.hasFirstOutTime {
				state.firstOutTimeMS = parsed
				state.hasFirstOutTime = true
			}
		}
	case "speed":
		state.speed = value
	case "total_size":
		state.totalSize = value
	}
	state.mu.Unlock()
}

// emitFFmpegRealtimeProgress 会把当前 FFmpeg 实时状态转换成对外进度日志。
func (r *screenshotRunner) emitFFmpegRealtimeProgress(status string, state *ffmpegRealtimeState, stage string, detailBuilder func(*ffmpegRealtimeState) string) {
	if status == "" {
		return
	}

	state.mu.Lock()
	defer state.mu.Unlock()

	percent := r.ffmpegProgressPercent(stage, status, state)
	detail := detailBuilder(state)
	if percent == state.lastLoggedPercent && detail == state.lastLoggedDetail {
		return
	}

	r.logProgressPercent(stage, percent, detail)
	state.lastLoggedPercent = percent
	state.lastLoggedDetail = detail
}

// ffmpegProgressPercent 会根据阶段和实时指标估算 FFmpeg 当前完成百分比。
func (r *screenshotRunner) ffmpegProgressPercent(stage, status string, state *ffmpegRealtimeState) float64 {
	if status == "end" {
		return 100
	}

	if stage == "字幕" {
		if processedSeconds, totalSeconds, ok := r.ffmpegSubtitleProcessedWindow(state); ok && totalSeconds > 0 {
			percent := processedSeconds / totalSeconds * 100
			if percent < 0.1 {
				percent = 0.1
			}
			return screenshotprogress.ClampPercent(minFloat(percent, 94))
		}
	}

	if stage == "渲染" {
		if percent, ok := approximateRenderProgressPercent(state); ok {
			return percent
		}
	}

	state.heartbeatCount++
	percent := 12 + state.heartbeatCount*8
	if strings.TrimSpace(state.speed) != "" {
		if percent < 26 {
			percent = 26
		}
	}
	if state.outTimeMS > 0 || strings.TrimSpace(state.totalSize) != "" {
		if percent < 48 {
			percent = 48
		}
	}
	if frame, err := strconv.Atoi(strings.TrimSpace(state.frame)); err == nil && frame > 0 {
		if percent < 78 {
			percent = 78
		}
	}
	return screenshotprogress.ClampPercent(minFloat(float64(percent), 94))
}

// approximateRenderProgressPercent 会优先根据输出时间或速度估算单帧渲染进度。
func approximateRenderProgressPercent(state *ffmpegRealtimeState) (float64, bool) {
	if state == nil || state.windowSeconds <= 0 {
		if percent, ok := approximateUnknownRenderProgressPercent(state); ok {
			return percent, true
		}
		return 0, false
	}

	if state.hasFirstOutTime && state.outTimeMS > state.firstOutTimeMS {
		processedSeconds := float64(state.outTimeMS-state.firstOutTimeMS) / 1_000_000.0
		if processedSeconds > 0 {
			percent := processedSeconds / state.windowSeconds * 100
			if percent < 0.1 {
				percent = 0.1
			}
			return screenshotprogress.ClampPercent(minFloat(percent, 94)), true
		}
	}

	speed, ok := parseFFmpegSpeed(state.speed)
	if !ok || speed <= 0 {
		return 0, false
	}
	elapsed := time.Since(state.startedAt).Seconds()
	if elapsed <= 0 {
		return 0, false
	}
	estimatedTotal := state.windowSeconds / speed
	if estimatedTotal <= 0 {
		if percent, ok := approximateUnknownRenderProgressPercent(state); ok {
			return percent, true
		}
		return 0, false
	}
	percent := elapsed / estimatedTotal * 100
	if percent < 0.1 {
		percent = 0.1
	}
	return screenshotprogress.ClampPercent(minFloat(percent, 94)), true
}

// approximateUnknownRenderProgressPercent 会在缺少稳定指标时用耗时平滑估算渲染进度。
func approximateUnknownRenderProgressPercent(state *ffmpegRealtimeState) (float64, bool) {
	if state == nil || state.startedAt.IsZero() {
		return 0, false
	}
	elapsed := time.Since(state.startedAt).Seconds()
	if elapsed <= 0 {
		return 0, false
	}

	// 单帧截图经常拿不到稳定的 ffmpeg 实时指标，这里用一个平滑的
	// elapsed-time 估算，让进度条持续前进但不会很快冲到头。
	estimate := 1.5
	if state.windowSeconds > 0 {
		estimate = maxFloat(estimate, minFloat(state.windowSeconds, 3.0))
	}

	percent := 94.0 * elapsed / (elapsed + estimate)
	if percent < 0.1 {
		percent = 0.1
	}
	return screenshotprogress.ClampPercent(percent), true
}

// ffmpegRenderProgressDetail 会生成截图渲染阶段的实时进度文案。
func (r *screenshotRunner) ffmpegRenderProgressDetail(state *ffmpegRealtimeState) string {
	base := r.activeRenderProgressLabel()
	return base + r.ffmpegProgressMetricsSuffix(state)
}

// ffmpegSubtitleProgressDetail 会生成字幕提取阶段的实时进度文案。
func (r *screenshotRunner) ffmpegSubtitleProgressDetail(state *ffmpegRealtimeState) string {
	base := "正在提取内封文字字幕。"
	if processedSeconds, totalSeconds, ok := r.ffmpegSubtitleProcessedWindow(state); ok {
		return fmt.Sprintf("%s | 已处理 %s / %s", base, screenshottimestamps.SecToHMS(processedSeconds), screenshottimestamps.SecToHMS(totalSeconds))
	}
	return base + r.ffmpegProgressMetricsSuffix(state)
}

// ffmpegSubtitleProcessedWindow 会把字幕提取实时状态换算成适合展示的已处理时长和总时长。
func (r *screenshotRunner) ffmpegSubtitleProcessedWindow(state *ffmpegRealtimeState) (float64, float64, bool) {
	if r == nil || state == nil || r.media.Duration <= 0 || state.outTimeMS <= 0 {
		return 0, 0, false
	}

	totalSeconds := r.media.Duration
	processedSeconds := float64(state.outTimeMS) / 1_000_000.0
	firstSeconds := float64(state.firstOutTimeMS) / 1_000_000.0

	if state.hasFirstOutTime && state.firstOutTimeMS > 0 && firstSeconds < totalSeconds {
		processedSeconds = float64(maxInt64(state.outTimeMS-state.firstOutTimeMS, 0)) / 1_000_000.0
		totalSeconds -= firstSeconds
	}
	if totalSeconds <= 0 {
		totalSeconds = r.media.Duration
	}
	if processedSeconds < 0 {
		processedSeconds = 0
	}
	if processedSeconds > totalSeconds {
		processedSeconds = totalSeconds
	}
	return processedSeconds, totalSeconds, totalSeconds > 0
}

// ffmpegProgressMetricsSuffix 会把 frame、fps、speed 等指标拼接成进度详情后缀。
func (r *screenshotRunner) ffmpegProgressMetricsSuffix(state *ffmpegRealtimeState) string {
	parts := make([]string, 0, 4)
	if isUsefulFFmpegFrame(state.frame) {
		parts = append(parts, "frame="+strings.TrimSpace(state.frame))
	}
	if isUsefulFFmpegFPS(state.fps) {
		parts = append(parts, "fps="+strings.TrimSpace(state.fps))
	}
	if strings.TrimSpace(state.outTime) != "" && r.activeShot.Phase() == "" {
		parts = append(parts, "time="+strings.TrimSpace(state.outTime))
	}
	if isUsefulFFmpegSpeed(state.speed) {
		parts = append(parts, "speed="+strings.TrimSpace(state.speed))
	}
	if len(parts) == 0 {
		return ""
	}
	return " | " + strings.Join(parts, " | ")
}

// activeRenderProgressLabel 会返回当前截图渲染阶段适合展示的说明文本。
func (r *screenshotRunner) activeRenderProgressLabel() string {
	if r == nil {
		return "正在渲染截图。"
	}
	return r.activeShot.ProgressLabel()
}

// logShotAlignmentProgress 会记录当前截图进入字幕对齐阶段的进度文案。
func (r *screenshotRunner) logShotAlignmentProgress() {
	if r == nil || !r.activeShot.Active() {
		return
	}
	r.logProgress("截图开始", r.activeShot.Current(), r.activeShot.Total(), r.activeShot.AlignmentDetail())
}

// logBitmapSubtitleVisibilityProgress 会记录当前截图进入位图字幕可见性校验阶段的进度文案。
func (r *screenshotRunner) logBitmapSubtitleVisibilityProgress() {
	if r == nil || !r.activeShot.Active() {
		return
	}
	label := "PGS/DVD"
	switch {
	case r.isPGSSubtitle():
		label = "PGS"
	case r.isDVDSubtitle():
		label = "DVD"
	}
	r.logProgress("截图开始", r.activeShot.Current(), r.activeShot.Total(), r.activeShot.BitmapVisibilityDetail(label))
}

// normalizeRenderProgressWindow 会把渲染窗口时长归一化到更稳定的估算范围。
func normalizeRenderProgressWindow(seconds float64) float64 {
	switch {
	case seconds <= 0:
		return 0.5
	case seconds < 0.5:
		return 0.5
	default:
		return seconds
	}
}

// minFloat 会返回两个浮点数中的较小值。
func minFloat(left, right float64) float64 {
	if left < right {
		return left
	}
	return right
}

// maxFloat 会返回两个浮点数中的较大值。
func maxFloat(left, right float64) float64 {
	if left > right {
		return left
	}
	return right
}

// maxInt64 会返回两个 int64 中的较大值。
func maxInt64(left, right int64) int64 {
	if left > right {
		return left
	}
	return right
}

// isUsefulFFmpegFrame 会判断 frame 指标是否可用于进度展示。
func isUsefulFFmpegFrame(raw string) bool {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return false
	}
	value, err := strconv.Atoi(trimmed)
	return err == nil && value > 0
}

// isUsefulFFmpegFPS 会判断 fps 指标是否可用于进度展示。
func isUsefulFFmpegFPS(raw string) bool {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || strings.EqualFold(trimmed, "n/a") {
		return false
	}
	value, err := strconv.ParseFloat(trimmed, 64)
	return err == nil && value > 0
}

// isUsefulFFmpegSpeed 会判断 speed 指标是否可用于进度展示。
func isUsefulFFmpegSpeed(raw string) bool {
	speed, ok := parseFFmpegSpeed(raw)
	return ok && speed > 0
}

// parseFFmpegSpeed 会把形如 2.3x 的 speed 文本解析成浮点倍速。
func parseFFmpegSpeed(raw string) (float64, bool) {
	trimmed := strings.TrimSpace(strings.TrimSuffix(raw, "x"))
	if trimmed == "" {
		return 0, false
	}
	value, err := strconv.ParseFloat(trimmed, 64)
	if err != nil || value <= 0 {
		return 0, false
	}
	return value, true
}
