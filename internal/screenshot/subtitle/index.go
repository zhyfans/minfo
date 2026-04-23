// Package subtitle 提供字幕索引吸附与区间合并辅助函数。

package subtitle

import (
	"fmt"
	"math"

	screenshotruntime "minfo/internal/screenshot/runtime"
)

// SnapFromIndex 使用预先建立的字幕索引把时间点吸附到最合适的区间。
func SnapFromIndex(target float64, spans []screenshotruntime.SubtitleSpan, epsilon float64) (float64, bool) {
	if len(spans) == 0 {
		return 0, false
	}

	bestAfterIndex := -1
	bestAfterDistance := math.MaxFloat64
	for _, span := range spans {
		if span.End < target-epsilon || span.Start > target+40 {
			continue
		}
		if target >= span.Start-epsilon && target <= span.End+epsilon {
			return clampInsideSpan(target, span, epsilon), true
		}
		if span.Start >= target {
			distance := span.Start - target
			if distance < bestAfterDistance {
				bestAfterDistance = distance
				bestAfterIndex = indexOfSpan(spans, span)
			}
		}
	}

	if bestAfterIndex >= 0 {
		return clampInsideSpan(spans[bestAfterIndex].Start+epsilon, spans[bestAfterIndex], epsilon), true
	}

	if span := spans[len(spans)-1]; span.End >= target-40 {
		return clampInsideSpan(span.End-epsilon, span, epsilon), true
	}
	return 0, false
}

func clampInsideSpan(value float64, span screenshotruntime.SubtitleSpan, epsilon float64) float64 {
	minValue := span.Start + epsilon
	maxValue := span.End - epsilon
	if maxValue < minValue {
		mid := span.Start + (span.End-span.Start)/2
		if mid < span.Start {
			mid = span.Start
		}
		if mid > span.End {
			mid = span.End
		}
		return mid
	}
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

// BitmapSnapPoint 返回位图字幕区间更适合截图的中点时间。
func BitmapSnapPoint(span screenshotruntime.SubtitleSpan, epsilon float64) float64 {
	return clampInsideSpan(span.Start+(span.End-span.Start)/2, span, epsilon)
}

// BitmapCandidateKey 把候选时间点归一化为毫秒级字符串键，便于去重和缓存。
func BitmapCandidateKey(value float64) string {
	return fmt.Sprintf("%.3f", value)
}

// MergeNearbySpans 会合并邻近字幕区间，并保留后续流程仍然需要的有效信息。
func MergeNearbySpans(spans []screenshotruntime.SubtitleSpan, maxGap float64) []screenshotruntime.SubtitleSpan {
	if len(spans) <= 1 {
		return spans
	}

	merged := make([]screenshotruntime.SubtitleSpan, 0, len(spans))
	current := spans[0]
	for _, span := range spans[1:] {
		if span.Start-current.End <= maxGap {
			if span.End > current.End {
				current.End = span.End
			}
			continue
		}
		merged = append(merged, current)
		current = span
	}
	merged = append(merged, current)
	return merged
}

func indexOfSpan(spans []screenshotruntime.SubtitleSpan, wanted screenshotruntime.SubtitleSpan) int {
	for index, span := range spans {
		if span == wanted {
			return index
		}
	}
	return -1
}
