// Package timestamps 提供截图时间点生成与媒体时长探测辅助函数。

package timestamps

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"minfo/internal/system"
)

func probeMediaInfoDuration(ctx context.Context, path string) (float64, error) {
	mediainfo, err := system.ResolveBin(system.MediaInfoBinaryPath)
	if err != nil {
		return 0, err
	}

	stdout, stderr, err := system.RunCommand(ctx, mediainfo, "--Output=General;%Duration%", path)
	if err != nil {
		return 0, fmt.Errorf("mediainfo duration probe failed: %s", system.BestErrorMessage(err, stderr, stdout))
	}
	return parseMediaInfoDurationOutput(stdout)
}

func parseMediaInfoDurationOutput(output string) (float64, error) {
	values := strings.FieldsFunc(output, func(r rune) bool {
		return r == '\n' || r == '\r' || r == ';'
	})
	invalid := make([]string, 0, len(values))

	for _, raw := range values {
		value := strings.TrimSpace(raw)
		if value == "" {
			continue
		}

		milliseconds, err := strconv.ParseFloat(value, 64)
		if err != nil || math.IsNaN(milliseconds) || math.IsInf(milliseconds, 0) || milliseconds <= 0 {
			invalid = append(invalid, value)
			continue
		}

		return milliseconds / 1000.0, nil
	}

	if len(invalid) == 0 {
		return 0, errors.New("mediainfo returned empty duration")
	}
	return 0, fmt.Errorf("mediainfo returned invalid duration values: %s", strings.Join(invalid, ", "))
}

func parseDurationOutput(output string) (float64, error) {
	lines := strings.Split(output, "\n")
	best := 0.0
	found := false
	invalid := make([]string, 0, len(lines))

	for _, line := range lines {
		value := strings.TrimSpace(line)
		if value == "" {
			continue
		}

		duration, err := strconv.ParseFloat(value, 64)
		if err != nil || math.IsNaN(duration) || math.IsInf(duration, 0) || duration <= 0 {
			invalid = append(invalid, value)
			continue
		}

		if !found || duration > best {
			best = duration
			found = true
		}
	}

	if found {
		return best, nil
	}
	if len(invalid) == 0 {
		return 0, errors.New("ffprobe returned empty duration")
	}
	return 0, fmt.Errorf("ffprobe returned invalid duration values: %s", strings.Join(invalid, ", "))
}

func firstFloatLine(output string) (float64, bool) {
	for _, line := range strings.Split(output, "\n") {
		if value, ok := parseFloatString(line); ok {
			return value, true
		}
	}
	return 0, false
}

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
