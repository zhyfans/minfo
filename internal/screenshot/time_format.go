// Package screenshot 提供截图流程共用的时间解析与格式化辅助函数。

package screenshot

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// parseRequestedTimestamps 把请求里的 HH:MM:SS 时间点列表转换为秒数切片。
func parseRequestedTimestamps(values []string) ([]float64, error) {
	result := make([]float64, 0, len(values))
	for _, value := range values {
		parsed, err := parseClockTimestamp(value)
		if err != nil {
			return nil, err
		}
		result = append(result, parsed)
	}
	return result, nil
}

// parseClockTimestamp 把单个 HH:MM:SS 时间戳解析成秒数。
func parseClockTimestamp(value string) (float64, error) {
	parts := strings.Split(strings.TrimSpace(value), ":")
	if len(parts) != 3 {
		return 0, fmt.Errorf("invalid timestamp %q", value)
	}

	hours, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, err
	}
	minutes, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, err
	}
	seconds, err := strconv.Atoi(parts[2])
	if err != nil {
		return 0, err
	}

	return float64(hours*3600 + minutes*60 + seconds), nil
}

// readInterval 按 ffprobe -read_intervals 需要的格式拼接起始时间和持续时长。
func readInterval(start, duration float64) string {
	return fmt.Sprintf("%s%%+%s", formatFloat(start), formatFloat(duration))
}

// formatFloat 把浮点数格式化为保留三位小数的字符串。
func formatFloat(value float64) string {
	return strconv.FormatFloat(value, 'f', 3, 64)
}

// formatTimestamp 把整秒值格式化为 HH:MM:SS。
func formatTimestamp(totalSeconds int) string {
	if totalSeconds < 0 {
		totalSeconds = 0
	}
	hours := totalSeconds / 3600
	minutes := (totalSeconds % 3600) / 60
	seconds := totalSeconds % 60
	return fmt.Sprintf("%02d:%02d:%02d", hours, minutes, seconds)
}

// secToHMS 把秒数向下取整后格式化为 HH:MM:SS。
func secToHMS(seconds float64) string {
	total := int(math.Floor(seconds))
	if total < 0 {
		total = 0
	}
	return formatTimestamp(total)
}

// secToFilenameStamp 把秒数格式化为截图文件名使用的时间戳片段。
func secToFilenameStamp(seconds float64) string {
	total := int(math.Floor(seconds))
	if total < 0 {
		total = 0
	}
	hours := total / 3600
	minutes := (total % 3600) / 60
	remain := total % 60
	return fmt.Sprintf("%02dh%02dm%02ds", hours, minutes, remain)
}

// secToHMSMS 把秒数格式化为带毫秒的 HH:MM:SS.mmm。
func secToHMSMS(seconds float64) string {
	if seconds < 0 {
		seconds = 0
	}
	hours := int(seconds / 3600)
	minutes := int(math.Mod(seconds, 3600) / 60)
	remain := seconds - float64(hours*3600+minutes*60)
	return fmt.Sprintf("%02d:%02d:%06.3f", hours, minutes, remain)
}

// displayProbeValue 把空值或 unknown 类探测结果统一显示为“无”。
func displayProbeValue(value string) string {
	lower := strings.ToLower(strings.TrimSpace(value))
	switch lower {
	case "", "unknown", "und", "undefined", "null", "n/a", "na":
		return "无"
	default:
		return strings.TrimSpace(value)
	}
}

// uniqueScreenshotName 为同一秒桶内的截图生成不冲突的文件名。
func uniqueScreenshotName(aligned float64, ext string, used map[string]int) string {
	base := secToFilenameStamp(aligned)
	count := used[base]
	used[base] = count + 1
	if count == 0 {
		return base + ext
	}
	return fmt.Sprintf("%s-%d%s", base, count+1, ext)
}

// screenshotSecond 返回时间点对应的非负整秒桶。
func screenshotSecond(value float64) int {
	second := int(math.Floor(value))
	if second < 0 {
		return 0
	}
	return second
}
