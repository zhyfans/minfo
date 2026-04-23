// Package progress 提供截图子模块共用的阶段进度与心跳辅助函数。

package progress

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"
)

// StartHeartbeat 会按固定频率触发心跳回调，并在上下文结束或手动停止时退出。
func StartHeartbeat(ctx context.Context, onTick func(elapsed time.Duration)) func() {
	if onTick == nil {
		return func() {}
	}

	startedAt := time.Now()
	done := make(chan struct{})
	var ctxDone <-chan struct{}
	if ctx != nil {
		ctxDone = ctx.Done()
	}

	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctxDone:
				return
			case <-done:
				return
			case <-ticker.C:
				onTick(time.Since(startedAt))
			}
		}
	}()

	return func() {
		select {
		case <-done:
			return
		default:
			close(done)
		}
	}
}

// ClampPercent 会把进度百分比限制到 0-100，并统一保留一位小数精度。
func ClampPercent(percent float64) float64 {
	switch {
	case percent < 0:
		return 0
	case percent > 100:
		return 100
	default:
		return math.Round(percent*10) / 10
	}
}

// SubtitleHeartbeatStepPercent 会根据已耗时长估算字幕耗时步骤的心跳进度。
func SubtitleHeartbeatStepPercent(elapsed time.Duration) float64 {
	if elapsed <= 0 {
		return 0
	}

	seconds := elapsed.Seconds()
	progress := 94.0 * seconds / (seconds + 8)
	return ClampPercent(progress)
}

// SubtitleHeartbeatDetail 会把基础说明和已耗时信息拼接成心跳进度详情。
func SubtitleHeartbeatDetail(detail string, elapsed time.Duration) string {
	detail = strings.TrimSpace(detail)
	if detail == "" {
		return "正在处理字幕元数据。"
	}
	return fmt.Sprintf("%s | 已耗时 %s", detail, formatElapsedCompact(elapsed))
}

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
