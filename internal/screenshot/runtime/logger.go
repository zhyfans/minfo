package runtime

import (
	"fmt"
	"strings"
)

// LineHandler 处理截图流程产生的单行实时日志。
type LineHandler func(line string)

// Logger 维护截图运行期的日志缓存和实时分发回调。
type Logger struct {
	lines   []string
	handler LineHandler
}

// NewLogger 会基于可选的实时回调创建一个新的运行期日志器。
func NewLogger(handler LineHandler) Logger {
	return Logger{handler: handler}
}

// Addf 会追加一条格式化日志，并在存在回调时立即向外推送。
func (l *Logger) Addf(format string, args ...interface{}) {
	if l == nil {
		return
	}
	line := fmt.Sprintf(format, args...)
	l.lines = append(l.lines, line)
	if l.handler != nil {
		l.handler(line)
	}
}

// Text 会返回当前日志器累计的完整日志文本。
func (l *Logger) Text() string {
	if l == nil {
		return ""
	}
	return strings.TrimSpace(strings.Join(l.lines, "\n"))
}
