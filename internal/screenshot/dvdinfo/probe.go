// Package dvdinfo 提供 DVD mediainfo 探测逻辑。

package dvdinfo

import (
	"context"
	"fmt"
	"sort"
	"strings"

	screenshotruntime "minfo/internal/screenshot/runtime"
	"minfo/internal/system"
)

// Probe 使用 mediainfo 提取 DVD 字幕元数据；必要时会从 BUP 结果补齐缺失语言。
func Probe(ctx context.Context, mediainfoBin, path, probePath string) (screenshotruntime.DVDMediaInfoResult, error) {
	if strings.TrimSpace(mediainfoBin) == "" {
		return screenshotruntime.DVDMediaInfoResult{}, fmt.Errorf("mediainfo not available")
	}

	selectedVOBPath := ResolveVOBPath(path, probePath)
	primaryPath := ResolveProbePath(path, probePath)
	result, err := probeOnce(ctx, mediainfoBin, primaryPath)
	if err != nil {
		return screenshotruntime.DVDMediaInfoResult{}, err
	}
	result.ProbePath = primaryPath
	result.SelectedVOBPath = selectedVOBPath

	if NeedsLanguageFallback(result) {
		if bupPath, ok := BUPPath(primaryPath); ok {
			fallback, fallbackErr := probeOnce(ctx, mediainfoBin, bupPath)
			if fallbackErr == nil {
				merged, used := MergeLanguageFallback(result, fallback)
				if used {
					merged.ProbePath = result.ProbePath
					merged.SelectedVOBPath = result.SelectedVOBPath
					merged.LanguageFallbackPath = bupPath
					result = merged
				}
			}
		}
	}
	return result, nil
}

func probeOnce(ctx context.Context, mediainfoBin, path string) (screenshotruntime.DVDMediaInfoResult, error) {
	stdout, stderr, err := system.RunCommand(ctx, mediainfoBin, "--Output=JSON", path)
	if err != nil {
		return screenshotruntime.DVDMediaInfoResult{}, fmt.Errorf(system.BestErrorMessage(err, stderr, stdout))
	}
	if strings.TrimSpace(stdout) == "" {
		return screenshotruntime.DVDMediaInfoResult{}, fmt.Errorf("mediainfo returned empty output")
	}

	payloads, err := decodePayloads([]byte(stdout))
	if err != nil {
		return screenshotruntime.DVDMediaInfoResult{}, err
	}

	result := screenshotruntime.DVDMediaInfoResult{Tracks: make([]screenshotruntime.DVDMediaInfoTrack, 0, 8)}
	for _, payload := range payloads {
		for _, track := range payload.Media.Track {
			trackType := strings.TrimSpace(trackString(track, "@type"))
			switch strings.ToLower(trackType) {
			case "general":
				if value, ok := parseTrackDuration(trackString(track, "Duration")); ok && value > 0 && value > result.Duration {
					result.Duration = value
				}
				if result.DisplayAspectRatio == "" {
					result.DisplayAspectRatio = normalizeAspectRatio(trackString(track, "DisplayAspectRatio", "DisplayAspectRatio/String"))
				}
			case "video":
				if result.DisplayAspectRatio == "" {
					result.DisplayAspectRatio = normalizeAspectRatio(trackString(track, "DisplayAspectRatio", "DisplayAspectRatio/String"))
				}
			case "text":
				streamID, _ := parseStreamID(trackString(track, "ID"))
				result.Tracks = append(result.Tracks, screenshotruntime.DVDMediaInfoTrack{
					StreamID: streamID,
					ID:       trackString(track, "ID"),
					Format:   trackString(track, "Format", "Format/String"),
					Language: trackString(track, "Language", "Language/String"),
					Title:    trackString(track, "Title", "Title/String"),
					Source:   trackString(track, "Source"),
				})
			}
		}
	}

	sort.Slice(result.Tracks, func(i, j int) bool {
		if result.Tracks[i].StreamID != result.Tracks[j].StreamID {
			return result.Tracks[i].StreamID < result.Tracks[j].StreamID
		}
		return result.Tracks[i].ID < result.Tracks[j].ID
	})
	return result, nil
}
