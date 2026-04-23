// Package timestamps 提供截图时间点生成、媒体时长探测以及时间格式化辅助函数。

package timestamps

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// ParseRequestedTimestamps 把请求里的 HH:MM:SS 时间点列表转换为秒数切片。
func ParseRequestedTimestamps(values []string) ([]float64, error) {
	result := make([]float64, 0, len(values))
	for _, value := range values {
		parsed, err := ParseClockTimestamp(value)
		if err != nil {
			return nil, err
		}
		result = append(result, parsed)
	}
	return result, nil
}

// ParseClockTimestamp 把单个 HH:MM:SS 时间戳解析成秒数。
func ParseClockTimestamp(value string) (float64, error) {
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

// ReadInterval 按 ffprobe -read_intervals 需要的格式拼接起始时间和持续时长。
func ReadInterval(start, duration float64) string {
	return fmt.Sprintf("%s%%+%s", FormatFloat(start), FormatFloat(duration))
}

// FormatFloat 把浮点数格式化为保留三位小数的字符串。
func FormatFloat(value float64) string {
	return strconv.FormatFloat(value, 'f', 3, 64)
}

// FormatTimestamp 把整秒值格式化为 HH:MM:SS。
func FormatTimestamp(totalSeconds int) string {
	if totalSeconds < 0 {
		totalSeconds = 0
	}
	hours := totalSeconds / 3600
	minutes := (totalSeconds % 3600) / 60
	seconds := totalSeconds % 60
	return fmt.Sprintf("%02d:%02d:%02d", hours, minutes, seconds)
}

// SecToHMS 把秒数向下取整后格式化为 HH:MM:SS。
func SecToHMS(seconds float64) string {
	total := int(math.Floor(seconds))
	if total < 0 {
		total = 0
	}
	return FormatTimestamp(total)
}

// SecToFilenameStamp 把秒数格式化为截图文件名使用的时间戳片段。
func SecToFilenameStamp(seconds float64) string {
	total := int(math.Floor(seconds))
	if total < 0 {
		total = 0
	}
	hours := total / 3600
	minutes := (total % 3600) / 60
	remain := total % 60
	return fmt.Sprintf("%02dh%02dm%02ds", hours, minutes, remain)
}

// SecToHMSMS 把秒数格式化为带毫秒的 HH:MM:SS.mmm。
func SecToHMSMS(seconds float64) string {
	if seconds < 0 {
		seconds = 0
	}
	hours := int(seconds / 3600)
	minutes := int(math.Mod(seconds, 3600) / 60)
	remain := seconds - float64(hours*3600+minutes*60)
	return fmt.Sprintf("%02d:%02d:%06.3f", hours, minutes, remain)
}

// DisplayProbeValue 把空值或 unknown 类探测结果统一显示为“无”。
func DisplayProbeValue(value string) string {
	lower := strings.ToLower(strings.TrimSpace(value))
	switch lower {
	case "", "unknown", "und", "undefined", "null", "n/a", "na":
		return "无"
	default:
		return strings.TrimSpace(value)
	}
}

// UniqueScreenshotName 为同一秒桶内的截图生成不冲突的文件名。
func UniqueScreenshotName(aligned float64, ext string, used map[string]int) string {
	base := SecToFilenameStamp(aligned)
	count := used[base]
	used[base] = count + 1
	if count == 0 {
		return base + ext
	}
	return fmt.Sprintf("%s-%d%s", base, count+1, ext)
}

// ScreenshotSecond 返回时间点对应的非负整秒桶。
func ScreenshotSecond(value float64) int {
	second := int(math.Floor(value))
	if second < 0 {
		return 0
	}
	return second
}
