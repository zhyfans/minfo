package runtime

import "testing"

// TestLoggerAddfAndText 验证运行期日志器会同时积累日志并推送实时回调。
func TestLoggerAddfAndText(t *testing.T) {
	seen := make([]string, 0, 2)
	logger := NewLogger(func(line string) {
		seen = append(seen, line)
	})

	logger.Addf("第一行")
	logger.Addf("第二行：%d", 2)

	if got := logger.Text(); got != "第一行\n第二行：2" {
		t.Fatalf("Text() = %q, want joined logs", got)
	}
	if len(seen) != 2 || seen[0] != "第一行" || seen[1] != "第二行：2" {
		t.Fatalf("callback lines = %#v, want both emitted lines", seen)
	}
}
