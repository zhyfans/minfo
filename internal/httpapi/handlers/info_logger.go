package handlers

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"minfo/internal/httpapi/transport"
	"minfo/internal/system"
)

type infoLogger struct {
	mu    sync.Mutex
	lines []timedLogLine
}

type timedLogLine struct {
	timestamp time.Time
	message   string
}

// newInfoLogger 会创建一个带时间戳缓存的请求日志记录器。
func newInfoLogger() *infoLogger {
	return &infoLogger{
		lines: make([]timedLogLine, 0, 32),
	}
}

// Logf 会记录格式化日志。
func (l *infoLogger) Logf(format string, args ...any) {
	if l == nil {
		return
	}
	now := time.Now()
	line := fmt.Sprintf(format, args...)
	l.mu.Lock()
	l.lines = append(l.lines, timedLogLine{
		timestamp: now,
		message:   line,
	})
	l.mu.Unlock()
}

// LogLine 会按原样记录单行日志。
func (l *infoLogger) LogLine(line string) {
	if l == nil {
		return
	}
	l.Logf("%s", line)
}

// LogMultiline 会按行拆分多行文本，并为每一行补上统一前缀。
func (l *infoLogger) LogMultiline(prefix, text string) {
	if l == nil {
		return
	}
	for _, line := range splitLogLines(text) {
		if prefix == "" {
			l.Logf("%s", line)
			continue
		}
		l.Logf("%s%s", prefix, line)
	}
}

// CommandOutput 返回一个命令输出回调，负责把 stdout 和 stderr 逐行写入日志。
func (l *infoLogger) CommandOutput(scope string) system.OutputLineHandler {
	return func(stream, line string) {
		l.Logf("[%s][%s] %s", scope, stream, line)
	}
}

// String 返回当前请求已经累积的完整日志文本。
func (l *infoLogger) String() string {
	if l == nil {
		return ""
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if len(l.lines) == 0 {
		return ""
	}

	formatted := make([]string, 0, len(l.lines))
	for _, line := range l.lines {
		if line.timestamp.IsZero() {
			formatted = append(formatted, line.message)
			continue
		}
		formatted = append(formatted, fmt.Sprintf("[%s] %s", line.timestamp.Format("15:04:05"), line.message))
	}
	return strings.Join(formatted, "\n")
}

// Entries 返回当前请求已经累积的结构化日志列表，供前端按本地时区重新格式化。
func (l *infoLogger) Entries() []transport.LogEntry {
	if l == nil {
		return nil
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if len(l.lines) == 0 {
		return nil
	}

	entries := make([]transport.LogEntry, 0, len(l.lines))
	for _, line := range l.lines {
		entry := transport.LogEntry{
			Message: line.message,
		}
		if !line.timestamp.IsZero() {
			entry.Timestamp = line.timestamp.UTC().Format(time.RFC3339Nano)
		}
		entries = append(entries, entry)
	}
	return entries
}

// Close 会释放当前日志记录器持有的资源。
func (l *infoLogger) Close() {
}

// writeInfoError 会把统一格式的错误响应连同当前日志一起写回客户端。
func writeInfoError(w http.ResponseWriter, status int, message string, logger *infoLogger) {
	transport.WriteJSON(w, status, transport.InfoResponse{
		OK:         false,
		Error:      message,
		Logs:       logger.String(),
		LogEntries: logger.Entries(),
	})
}

// splitLogLines 会把混合换行符的文本拆成稳定的逐行结果。
func splitLogLines(text string) []string {
	normalized := strings.ReplaceAll(text, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")
	if normalized == "" {
		return []string{""}
	}
	return strings.Split(normalized, "\n")
}
