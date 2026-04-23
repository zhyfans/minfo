// Package timestamps 提供截图时间点生成与媒体时长探测辅助函数。

package timestamps

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	screenshotruntime "minfo/internal/screenshot/runtime"
	"minfo/internal/system"
)

const dvdPacketDiscontinuityGap = 30.0

var dvdTitleVOBPattern = regexp.MustCompile(`(?i)^VTS_(\d{2})_([1-9]\d*)\.VOB$`)

// ProbeMediaDuration 优先通过 ffprobe 探测时长；必要时回退到 DVD 包时长或 MediaInfo。
func ProbeMediaDuration(ctx context.Context, ffprobe, path string) (float64, error) {
	if isDVDTitleVOB(path) {
		if duration, err := probeDVDTitleVOBPacketDuration(ctx, ffprobe, path); err == nil {
			return duration, nil
		}
	}

	stdout, stderr, err := runFFprobeDuration(ctx, ffprobe, path, "format=duration")
	if err != nil {
		return 0, fmt.Errorf("ffprobe format duration probe failed: %s", system.BestErrorMessage(err, stderr, stdout))
	}

	duration, parseErr := parseDurationOutput(stdout)
	if parseErr == nil {
		return duration, nil
	}

	stdout, stderr, err = runFFprobeDuration(ctx, ffprobe, path, "stream=duration")
	if err != nil {
		return 0, fmt.Errorf("ffprobe format duration unavailable (%v); stream duration probe failed: %s", parseErr, system.BestErrorMessage(err, stderr, stdout))
	}

	duration, streamErr := parseDurationOutput(stdout)
	if streamErr == nil {
		return duration, nil
	}

	if duration, mediaErr := probeMediaInfoDuration(ctx, path); mediaErr == nil {
		return duration, nil
	}

	return 0, fmt.Errorf("ffprobe returned unusable duration: format probe (%v); stream probe (%v)", parseErr, streamErr)
}

func isDVDTitleVOB(path string) bool {
	return dvdTitleVOBPattern.MatchString(filepath.Base(strings.TrimSpace(path)))
}

func runFFprobeDuration(ctx context.Context, ffprobe, path, entries string) (string, string, error) {
	return system.RunCommand(ctx, ffprobe,
		"-v", "error",
		"-show_entries", entries,
		"-of", "default=noprint_wrappers=1:nokey=1",
		path,
	)
}

func probeDVDTitleVOBPacketDuration(ctx context.Context, ffprobe, path string) (float64, error) {
	startOffset, err := probeVideoStartOffset(ctx, ffprobe, path)
	if err != nil {
		return 0, err
	}

	stdout, stderr, err := system.RunCommand(ctx, ffprobe,
		"-v", "error",
		"-select_streams", "v:0",
		"-show_packets",
		"-show_entries", "packet=pts_time,duration_time",
		"-of", "json",
		path,
	)
	if err != nil {
		return 0, fmt.Errorf(system.BestErrorMessage(err, stderr, stdout))
	}
	if strings.TrimSpace(stdout) == "" {
		return 0, errors.New("ffprobe returned empty packet payload")
	}

	var payload screenshotruntime.FFprobePacketsPayload
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		return 0, err
	}

	duration, ok := accumulateDVDPacketDuration(payload.Packets, startOffset, dvdPacketDiscontinuityGap)
	if !ok || duration <= 0 {
		return 0, errors.New("ffprobe returned unusable packet duration")
	}
	return duration, nil
}

func probeVideoStartOffset(ctx context.Context, ffprobe, path string) (float64, error) {
	stdout, stderr, err := system.RunCommand(ctx, ffprobe,
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=start_time",
		"-of", "default=noprint_wrappers=1:nokey=1",
		path,
	)
	if err == nil {
		if value, ok := firstFloatLine(stdout); ok {
			return value, nil
		}
	}

	stdout, stderr, err = system.RunCommand(ctx, ffprobe,
		"-v", "error",
		"-show_entries", "format=start_time",
		"-of", "default=noprint_wrappers=1:nokey=1",
		path,
	)
	if err == nil {
		if value, ok := firstFloatLine(stdout); ok {
			return value, nil
		}
	}
	if err != nil {
		return 0, fmt.Errorf(system.BestErrorMessage(err, stderr, stdout))
	}
	return 0, errors.New("ffprobe returned empty start_time")
}

func accumulateDVDPacketDuration(packets []screenshotruntime.FFprobePacket, startOffset, discontinuityGap float64) (float64, bool) {
	if discontinuityGap <= 0 {
		discontinuityGap = dvdPacketDiscontinuityGap
	}

	clusterStart := 0.0
	clusterEnd := 0.0
	total := 0.0
	started := false

	for _, packet := range packets {
		pts, ok := parseFloatString(packet.PTSTime)
		if !ok {
			continue
		}
		durationValue, ok := parseFloatString(packet.DurationTime)
		if !ok || durationValue < 0 {
			durationValue = 0
		}

		packetStart := pts
		packetEnd := pts + durationValue
		if packetEnd < packetStart {
			packetEnd = packetStart
		}

		if !started {
			clusterStart = minFloat(startOffset, packetStart)
			clusterEnd = packetEnd
			started = true
			continue
		}

		if packetStart > clusterEnd+discontinuityGap || packetEnd < clusterStart-discontinuityGap || packetStart < clusterStart-discontinuityGap {
			if clusterEnd > clusterStart {
				total += clusterEnd - clusterStart
			}
			clusterStart = packetStart
			clusterEnd = packetEnd
			continue
		}

		if packetStart < clusterStart {
			clusterStart = packetStart
		}
		if packetEnd > clusterEnd {
			clusterEnd = packetEnd
		}
	}

	if !started {
		return 0, false
	}
	if clusterEnd > clusterStart {
		total += clusterEnd - clusterStart
	}
	if total <= 0 {
		return 0, false
	}
	return normalizeDuration(total), total > 0
}
