// Package timestamps 提供截图时间点生成与媒体时长探测辅助函数。

package timestamps

import (
	"context"
	"math"
	"math/rand"
	"sort"
	"time"

	"minfo/internal/system"
)

// RandomSecondsForSource 针对已经解析好的媒体源生成随机截图秒数。
func RandomSecondsForSource(ctx context.Context, sourcePath string, count int) ([]int, error) {
	ffprobe, err := system.ResolveBin(system.FFprobeBinaryPath)
	if err != nil {
		return nil, err
	}

	duration, err := ProbeMediaDuration(ctx, ffprobe, sourcePath)
	if err != nil {
		return nil, err
	}

	return BuildRandomSeconds(duration, count), nil
}

// RandomTimestampsForSource 针对已经解析好的媒体源生成 HH:MM:SS 格式的随机截图时间点。
func RandomTimestampsForSource(ctx context.Context, sourcePath string, count int) ([]string, error) {
	seconds, err := RandomSecondsForSource(ctx, sourcePath, count)
	if err != nil {
		return nil, err
	}

	timestamps := make([]string, 0, len(seconds))
	for _, second := range seconds {
		timestamps = append(timestamps, FormatTimestamp(second))
	}
	return timestamps, nil
}

// BuildRandomSeconds 会在媒体时长范围内按分段随机的方式生成截图秒数。
func BuildRandomSeconds(duration float64, count int) []int {
	start := 0.0
	end := duration
	if duration > 120 {
		margin := duration * 0.08
		if margin < 15 {
			margin = 15
		}
		if margin > 300 {
			margin = 300
		}
		start = margin
		end = duration - margin
		if end <= start {
			start = 0
			end = duration
		}
	}

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	step := (end - start) / float64(count)
	if step <= 0 {
		step = duration / float64(count+1)
	}

	values := make([]int, 0, count)
	used := make(map[int]struct{}, count)
	for index := 0; index < count; index++ {
		segmentStart := start + step*float64(index)
		segmentEnd := segmentStart + step
		if index == count-1 || segmentEnd > end {
			segmentEnd = end
		}
		if segmentEnd <= segmentStart {
			segmentEnd = segmentStart + 1
		}

		value := int(segmentStart + rng.Float64()*(segmentEnd-segmentStart))
		if value < 0 {
			value = 0
		}
		maxSecond := int(duration)
		if maxSecond > 0 && value >= maxSecond {
			value = maxSecond - 1
		}
		for try := 0; try < 8; try++ {
			if _, exists := used[value]; !exists {
				break
			}
			value++
		}
		used[value] = struct{}{}
		values = append(values, value)
	}

	sort.Ints(values)
	return values
}

func minFloat(left, right float64) float64 {
	if left < right {
		return left
	}
	return right
}

func normalizeDuration(duration float64) float64 {
	if duration < 0 || math.IsNaN(duration) || math.IsInf(duration, 0) {
		return 0
	}
	return duration
}
