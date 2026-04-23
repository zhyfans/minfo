// Package system 提供外部命令执行和实时输出转发能力。

package system

import (
	"bytes"
	"context"
	"strings"
	"sync"
)

// FormatCommandForLog 会把命令、参数和当前执行层注入的关键环境变量格式化成便于日志展示的字符串。
func FormatCommandForLog(ctx context.Context, bin string, args ...string) string {
	parts := make([]string, 0, len(args)+1+len(commandEnvOverrides(ctx, bin)))
	for _, override := range commandEnvOverrides(ctx, bin) {
		parts = append(parts, quoteArg(override.key+"="+override.value))
	}
	parts = append(parts, quoteArg(bin))
	for _, arg := range args {
		parts = append(parts, quoteArg(arg))
	}
	return strings.Join(parts, " ")
}

// quoteArg 会按 shell 友好的方式转义单个命令参数，便于日志直观展示。
func quoteArg(value string) string {
	if value == "" {
		return "''"
	}
	if strings.IndexFunc(value, func(r rune) bool {
		switch r {
		case ' ', '\t', '\n', '\r', '\'', '"', '\\', '|', '&', ';', '<', '>', '(', ')', '[', ']', '{', '}', '$':
			return true
		default:
			return false
		}
	}) == -1 {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

type lineRelayWriter struct {
	mu     sync.Mutex
	stream string
	onLine OutputLineHandler
	buffer bytes.Buffer
}

// newLineRelayWriter 创建一个按行拆分并转发输出的 Writer。
func newLineRelayWriter(stream string, onLine OutputLineHandler) *lineRelayWriter {
	return &lineRelayWriter{
		stream: stream,
		onLine: onLine,
	}
}

// Write 把命令输出写入缓冲区，并在读到完整行时立即回调。
func (w *lineRelayWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if _, err := w.buffer.Write(p); err != nil {
		return 0, err
	}
	w.flushCompleteLinesLocked()
	return len(p), nil
}

// Flush 会刷新缓冲内容，确保待发送数据及时写出。
func (w *lineRelayWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()

	remaining := strings.TrimRight(w.buffer.String(), "\r\n")
	if remaining != "" || w.buffer.Len() > 0 {
		w.onLine(w.stream, remaining)
	}
	w.buffer.Reset()
}

// flushCompleteLinesLocked 假定调用方已持锁，并把缓冲区中完整的行逐一回调出去。
func (w *lineRelayWriter) flushCompleteLinesLocked() {
	data := w.buffer.Bytes()
	start := 0
	for idx := 0; idx < len(data); idx++ {
		ch := data[idx]
		if ch != '\n' && ch != '\r' {
			continue
		}
		line := strings.TrimRight(string(data[start:idx]), "\r")
		w.onLine(w.stream, line)
		if ch == '\r' && idx+1 < len(data) && data[idx+1] == '\n' {
			start = idx + 2
			idx++
			continue
		}
		start = idx + 1
	}
	if start == 0 {
		return
	}
	w.buffer.Next(start)
}

// BestErrorMessage 组合 err、stderr 和 stdout，生成更适合展示给用户的错误文本。
func BestErrorMessage(err error, stderr, stdout string) string {
	msg := strings.TrimSpace(stderr)
	if msg == "" {
		msg = err.Error()
	}
	if strings.TrimSpace(stdout) != "" {
		msg += "\n\n" + strings.TrimSpace(stdout)
	}
	return msg
}

// CombineCommandOutput 合并 stdout 和 stderr，并保留两者的可读分隔。
func CombineCommandOutput(stdout, stderr string) string {
	output := strings.TrimSpace(stdout)
	if strings.TrimSpace(stderr) != "" {
		if output != "" {
			output += "\n\n"
		}
		output += strings.TrimSpace(stderr)
	}
	return output
}
