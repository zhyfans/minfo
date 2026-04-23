// Package screenshot 提供截图流程共用的日志与进度辅助函数。

package screenshot

import (
	"fmt"
	"math"
	"strings"
	"time"

	"minfo/internal/taskprogress"
)

// logs 返回当前截图运行器已经累积的完整日志文本。
func (r *screenshotRunner) logs() string {
	if r == nil {
		return ""
	}
	return r.logger.Text()
}

// logf 会把一条格式化日志写入运行器缓存，并在存在实时回调时立即推送。
func (r *screenshotRunner) logf(format string, args ...interface{}) {
	if r == nil {
		return
	}
	r.logger.Addf(format, args...)
}

// logProgress 会写入一条稳定格式的进度日志，供上层推导阶段型进度。
func (r *screenshotRunner) logProgress(stage string, current, total int, detail string) {
	r.logf("%s", taskprogress.FormatStep(stage, current, total, detail))
}

// logProgressPercent 会写入一条带百分比的进度日志，适合外部工具实时进度。
func (r *screenshotRunner) logProgressPercent(stage string, percent float64, detail string) {
	r.logf("%s", taskprogress.FormatPercent(stage, percent, detail))
}

// EmitProgressLog 会通过实时日志回调输出一条统一格式的进度日志。
func EmitProgressLog(onLog LogHandler, stage string, current, total int, detail string) {
	if onLog == nil {
		return
	}
	onLog(taskprogress.FormatStep(stage, current, total, detail))
}

// EmitProgressPercentLog 会通过实时日志回调输出一条带百分比的进度日志。
func EmitProgressPercentLog(onLog LogHandler, stage string, percent float64, detail string) {
	if onLog == nil {
		return
	}
	onLog(taskprogress.FormatPercent(stage, percent, detail))
}

// clampProgressPercent 会把进度百分比限制到 0-100，并统一保留一位小数精度。
func clampProgressPercent(percent float64) float64 {
	switch {
	case percent < 0:
		return 0
	case percent > 100:
		return 100
	default:
		return math.Round(percent*10) / 10
	}
}

// subtitleHeartbeatStepPercent 会根据已耗时长估算字幕耗时步骤的心跳进度。
func subtitleHeartbeatStepPercent(elapsed time.Duration) float64 {
	if elapsed <= 0 {
		return 0
	}

	seconds := elapsed.Seconds()
	progress := 94.0 * seconds / (seconds + 8)
	return clampProgressPercent(progress)
}

// subtitleHeartbeatDetail 会把基础说明和已耗时信息拼接成心跳进度详情。
func subtitleHeartbeatDetail(detail string, elapsed time.Duration) string {
	detail = strings.TrimSpace(detail)
	if detail == "" {
		return "正在处理字幕元数据。"
	}
	return fmt.Sprintf("%s | 已耗时 %s", detail, formatElapsedCompact(elapsed))
}

// formatElapsedCompact 会把耗时格式化成适合进度日志的紧凑文本。
func formatElapsedCompact(elapsed time.Duration) string {
	if elapsed < 0 {
		elapsed = 0
	}

	seconds := int(math.Round(elapsed.Seconds()))
	if seconds < 1 {
		seconds = 1
	}
	if seconds < 60 {
		return fmt.Sprintf("%ds", seconds)
	}
	return fmt.Sprintf("%dm%02ds", seconds/60, seconds%60)
}
