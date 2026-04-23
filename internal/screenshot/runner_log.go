// Package screenshot 提供截图流程共用的运行器日志与进度辅助函数。

package screenshot

import (
	"strings"
	"time"

	screenshotprogress "minfo/internal/screenshot/progress"
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

// startProgressHeartbeat 会周期性输出阶段心跳进度，并返回停止函数。
func (r *screenshotRunner) startProgressHeartbeat(stage, detail string) func() {
	if r == nil || strings.TrimSpace(stage) == "" || strings.TrimSpace(detail) == "" {
		return func() {}
	}

	return screenshotprogress.StartHeartbeat(r.ctx, func(elapsed time.Duration) {
		r.logProgressPercent(stage, screenshotprogress.SubtitleHeartbeatStepPercent(elapsed), screenshotprogress.SubtitleHeartbeatDetail(detail, elapsed))
	})
}
