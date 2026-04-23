// Package screenshot 提供通用解析、路径转义和文件系统辅助函数。

package screenshot

import (
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// firstFloatLine 返回多行输出里第一个可解析的浮点数。
func firstFloatLine(output string) (float64, bool) {
	for _, line := range strings.Split(output, "\n") {
		if value, ok := parseFloatString(line); ok {
			return value, true
		}
	}
	return 0, false
}

// parseFloatString 会解析浮点值字符串，并把原始输入转换成结构化结果。
func parseFloatString(value string) (float64, bool) {
	text := strings.TrimSpace(value)
	if text == "" || text == "N/A" {
		return 0, false
	}
	parsed, err := strconv.ParseFloat(text, 64)
	if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
		return 0, false
	}
	return parsed, true
}

// parseIntString 会解析整数字符串，并把原始输入转换成结构化结果。
func parseIntString(value string) (int, bool) {
	text := strings.TrimSpace(value)
	if text == "" {
		return 0, false
	}
	parsed, err := strconv.Atoi(text)
	if err != nil {
		return 0, false
	}
	return parsed, true
}

// floatDiffGT 判断两个浮点数的差值是否超过容忍阈值。
func floatDiffGT(a, b float64) bool {
	return math.Abs(a-b) > 0.0005
}

// escapeFilterValue 转义 ffmpeg 字幕过滤器里使用的路径值。
func escapeFilterValue(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `'`, `\'`)
	value = strings.ReplaceAll(value, `:`, `\:`)
	value = strings.ReplaceAll(value, `,`, `\,`)
	value = strings.ReplaceAll(value, `;`, `\;`)
	value = strings.ReplaceAll(value, `[`, `\[`)
	value = strings.ReplaceAll(value, `]`, `\]`)
	return value
}

// clearDir 删除目录下的所有内容，但保留目录本身。
func clearDir(path string) error {
	entries, err := os.ReadDir(path)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := os.RemoveAll(filepath.Join(path, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

// allDigits 判断字符串是否非空且全部由数字组成。
func allDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, item := range value {
		if item < '0' || item > '9' {
			return false
		}
	}
	return true
}
