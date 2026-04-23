package handlers

import (
	"strings"

	"minfo/internal/httpapi/transport"
)

// pickRealtimeLogs 优先返回实时日志会话里已经收集的内容；为空时回退到 fallback。
func pickRealtimeLogs(logger *infoLogger, fallback string) string {
	if logger == nil {
		return fallback
	}
	if logs := logger.String(); strings.TrimSpace(logs) != "" {
		return logs
	}
	return fallback
}

// pickRealtimeLogEntries 优先返回实时日志会话里已经收集的结构化日志；为空时返回 nil。
func pickRealtimeLogEntries(logger *infoLogger) []transport.LogEntry {
	if logger == nil {
		return nil
	}
	return logger.Entries()
}
