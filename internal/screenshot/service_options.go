// Package screenshot 提供截图服务的参数规范化与独立进度心跳辅助。

package screenshot

import (
	"context"
	"strconv"
	"strings"
	"time"
)

// startStandaloneProgressHeartbeat 会通过外部日志回调周期性输出进度心跳，并返回停止函数。
func startStandaloneProgressHeartbeat(ctx context.Context, onLog LogHandler, stage, detail string) func() {
	if onLog == nil || strings.TrimSpace(stage) == "" || strings.TrimSpace(detail) == "" {
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
				elapsed := time.Since(startedAt)
				EmitProgressPercentLog(onLog, stage, subtitleHeartbeatStepPercent(elapsed), subtitleHeartbeatDetail(detail, elapsed))
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

// NormalizeMode 规范化截图接口的 mode；未知值会回落为 zip。
func NormalizeMode(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case ModeLinks:
		return ModeLinks
	default:
		return ModeZip
	}
}

// NormalizeVariant 规范化截图输出格式；未知值会回落为 png。
func NormalizeVariant(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case VariantJPG:
		return VariantJPG
	default:
		return VariantPNG
	}
}

// NormalizeSubtitleMode 规范化字幕模式；off 和 none 类输入会关闭字幕，其余值使用自动模式。
func NormalizeSubtitleMode(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case SubtitleModeOff, "none", "nosub", "false", "0":
		return SubtitleModeOff
	default:
		return SubtitleModeAuto
	}
}

// NormalizeCount 规范化截图数量，并限制在允许范围内。
func NormalizeCount(raw string) int {
	value := strings.TrimSpace(raw)
	if value == "" {
		return defaultScreenshotCount
	}

	count, err := strconv.Atoi(value)
	if err != nil {
		return defaultScreenshotCount
	}
	switch {
	case count < minScreenshotCount:
		return minScreenshotCount
	case count > maxScreenshotCount:
		return maxScreenshotCount
	default:
		return count
	}
}
