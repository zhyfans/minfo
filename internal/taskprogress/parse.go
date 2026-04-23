package taskprogress

import (
	"regexp"
	"strconv"
	"strings"
)

var (
	stepPattern    = regexp.MustCompile(`^\[进度\] ([^ ]+) (\d+)/(\d+): (.+)$`)
	percentPattern = regexp.MustCompile(`^\[进度\] ([^ ]+) (\d+(?:\.\d+)?)%: (.+)$`)
)

// ParseLogLine 会把统一格式的进度日志解析为结构化事件。
func ParseLogLine(line string) (Event, bool) {
	text := strings.TrimSpace(line)
	if text == "" {
		return Event{}, false
	}

	if matches := percentPattern.FindStringSubmatch(text); len(matches) == 4 {
		return Event{
			Kind:    KindPercent,
			Stage:   strings.TrimSpace(matches[1]),
			Percent: parseFloat(matches[2]),
			Detail:  strings.TrimSpace(matches[3]),
		}, true
	}

	if matches := stepPattern.FindStringSubmatch(text); len(matches) == 5 {
		return Event{
			Kind:    KindStep,
			Stage:   strings.TrimSpace(matches[1]),
			Current: parseInt(matches[2]),
			Total:   parseInt(matches[3]),
			Detail:  strings.TrimSpace(matches[4]),
		}, true
	}

	return Event{}, false
}

// parseInt 会把正则捕获到的整数文本转换为 int。
func parseInt(raw string) int {
	value, _ := strconv.Atoi(strings.TrimSpace(raw))
	return value
}

// parseFloat 会把正则捕获到的小数文本转换为 float64。
func parseFloat(raw string) float64 {
	value, _ := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	return value
}
