// Package screenshot 提供字幕索引吸附与区间整理辅助函数。

package screenshot

import (
	"math"
	"sort"
	"strconv"
)

// snapFromIndex 使用预先建立的字幕索引把时间点吸附到最合适的区间。
func snapFromIndex(target float64, spans []subtitleSpan, epsilon float64) (float64, bool) {
	if len(spans) == 0 {
		return target, false
	}

	bestAfterIndex := -1
	lastBeforeIndex := -1
	for index, span := range spans {
		if target >= span.Start && target <= span.End {
			return clampInsideSpan(target, span, epsilon), true
		}
		if bestAfterIndex == -1 && span.Start >= target {
			bestAfterIndex = index
		}
		if span.Start <= target {
			lastBeforeIndex = index
		}
	}

	if bestAfterIndex >= 0 {
		return clampInsideSpan(spans[bestAfterIndex].Start+epsilon, spans[bestAfterIndex], epsilon), true
	}
	if lastBeforeIndex >= 0 {
		span := spans[lastBeforeIndex]
		return clampInsideSpan(span.End-epsilon, span, epsilon), true
	}
	return target, false
}

// clampInsideSpan 把时间点限制在字幕区间内部，并预留 epsilon 安全边距。
func clampInsideSpan(value float64, span subtitleSpan, epsilon float64) float64 {
	if span.End <= span.Start {
		return span.Start
	}

	minValue := span.Start + epsilon
	maxValue := span.End - epsilon
	if maxValue < minValue {
		mid := span.Start + (span.End-span.Start)/2
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

// bitmapSnapPoint 返回位图字幕区间更适合截图的中点时间。
func bitmapSnapPoint(span subtitleSpan, epsilon float64) float64 {
	return clampInsideSpan(span.Start+(span.End-span.Start)/2, span, epsilon)
}

// bitmapCandidateKey 把候选时间点归一化为毫秒级字符串键，便于去重和缓存。
func bitmapCandidateKey(value float64) string {
	return strconv.FormatInt(int64(math.Round(value*1000)), 10)
}

// mergeNearbySubtitleSpans 会合并邻近字幕区间，并保留后续流程仍然需要的有效信息。
func mergeNearbySubtitleSpans(spans []subtitleSpan, maxGap float64) []subtitleSpan {
	if len(spans) <= 1 {
		return spans
	}
	if maxGap < 0 {
		maxGap = 0
	}

	sort.Slice(spans, func(i, j int) bool {
		if spans[i].Start == spans[j].Start {
			return spans[i].End < spans[j].End
		}
		return spans[i].Start < spans[j].Start
	})

	merged := make([]subtitleSpan, 0, len(spans))
	current := spans[0]
	for _, span := range spans[1:] {
		if span.Start <= current.End+maxGap {
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
