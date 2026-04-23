package screenshot

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"minfo/internal/system"
)

// detectStartOffset 读取当前截图源的起始时间偏移。
func (r *screenshotRunner) detectStartOffset() float64 {
	return r.detectStartOffsetForInput(r.sourcePath)
}

// detectStartOffsetForInput 读取指定输入的起始时间偏移。
func (r *screenshotRunner) detectStartOffsetForInput(input string) float64 {
	stdout, _, err := system.RunCommand(r.ctx, r.tools.FFprobeBin,
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=start_time",
		"-of", "default=noprint_wrappers=1:nokey=1",
		input,
	)
	if err == nil {
		if value, ok := firstFloatLine(stdout); ok {
			return value
		}
	}

	stdout, _, err = system.RunCommand(r.ctx, r.tools.FFprobeBin,
		"-v", "error",
		"-show_entries", "format=start_time",
		"-of", "default=noprint_wrappers=1:nokey=1",
		input,
	)
	if err == nil {
		if value, ok := firstFloatLine(stdout); ok {
			return value
		}
	}
	return 0
}

// detectVideoDimensions 读取当前视频流的宽高。
func (r *screenshotRunner) detectVideoDimensions() (int, int) {
	stdout, _, err := system.RunCommand(r.ctx, r.tools.FFprobeBin,
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=width,height",
		"-of", "csv=p=0:s=x",
		r.sourcePath,
	)
	if err != nil {
		return 0, 0
	}

	value := strings.TrimSpace(strings.SplitN(stdout, "\n", 2)[0])
	parts := strings.Split(value, "x")
	if len(parts) != 2 {
		return 0, 0
	}

	width, err1 := strconv.Atoi(parts[0])
	height, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return 0, 0
	}
	return width, height
}

// detectBitmapSubtitleCanvasDimensions 会读取当前位图字幕流声明的画布尺寸。
func (r *screenshotRunner) detectBitmapSubtitleCanvasDimensions() (int, int) {
	if r == nil || r.subtitle.Mode != "internal" || !r.isSupportedBitmapSubtitle() || r.subtitle.RelativeIndex < 0 {
		return 0, 0
	}

	stdout, _, err := system.RunCommand(r.ctx, r.tools.FFprobeBin,
		"-v", "error",
		"-select_streams", fmt.Sprintf("s:%d", r.subtitle.RelativeIndex),
		"-show_entries", "stream=width,height",
		"-of", "csv=p=0:s=x",
		r.sourcePath,
	)
	if err != nil {
		return 0, 0
	}

	value := strings.TrimSpace(strings.SplitN(stdout, "\n", 2)[0])
	parts := strings.Split(value, "x")
	if len(parts) != 2 {
		return 0, 0
	}

	width, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
	height, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err1 != nil || err2 != nil || width <= 0 || height <= 0 {
		return 0, 0
	}
	return width, height
}

// firstFloatLine 会返回输出文本里首个可用的浮点值。
func firstFloatLine(output string) (float64, bool) {
	for _, line := range strings.Split(output, "\n") {
		value := strings.TrimSpace(line)
		if value == "" || value == "N/A" {
			continue
		}
		parsed, err := strconv.ParseFloat(value, 64)
		if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
			continue
		}
		return parsed, true
	}
	return 0, false
}
