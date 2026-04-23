// Package progress 提供截图子模块共用的阶段进度与心跳辅助函数。

package progress

import "minfo/internal/taskprogress"

// EmitStepLog 会通过实时日志回调输出一条统一格式的步骤进度日志。
func EmitStepLog(onLog LineHandler, stage string, current, total int, detail string) {
	if onLog == nil {
		return
	}
	onLog(taskprogress.FormatStep(stage, current, total, detail))
}

// EmitPercentLog 会通过实时日志回调输出一条带百分比的进度日志。
func EmitPercentLog(onLog LineHandler, stage string, percent float64, detail string) {
	if onLog == nil {
		return
	}
	onLog(taskprogress.FormatPercent(stage, percent, detail))
}
