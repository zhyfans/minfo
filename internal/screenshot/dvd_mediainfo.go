// Package screenshot 提供 DVD mediainfo 探测入口与缓存逻辑。

package screenshot

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"minfo/internal/system"
)

// probeDVDMediaInfo 使用 mediainfo 提取 DVD 字幕元数据；必要时会从 BUP 结果补齐缺失语言。
func probeDVDMediaInfo(ctx context.Context, mediainfoBin, path, probePath string) (dvdMediaInfoResult, error) {
	if strings.TrimSpace(mediainfoBin) == "" {
		return dvdMediaInfoResult{}, fmt.Errorf("mediainfo not available")
	}

	selectedVOBPath := resolveDVDMediaInfoVOBPath(path, probePath)
	primaryPath := resolveDVDMediaInfoProbePath(path, probePath)
	result, err := probeDVDMediaInfoOnce(ctx, mediainfoBin, primaryPath)
	if err != nil {
		return dvdMediaInfoResult{}, err
	}
	result.ProbePath = primaryPath
	result.SelectedVOBPath = selectedVOBPath

	if dvdMediaInfoNeedsLanguageFallback(result) {
		if bupPath, ok := dvdMediaInfoBUPPath(primaryPath); ok {
			fallback, fallbackErr := probeDVDMediaInfoOnce(ctx, mediainfoBin, bupPath)
			if fallbackErr == nil {
				merged, used := mergeDVDMediaInfoLanguageFallback(result, fallback)
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

// ensureDVDMediaInfoResult 在 DVD 场景下只探测一次 mediainfo 结果，并缓存供后续字幕与比例逻辑复用。
func (r *screenshotRunner) ensureDVDMediaInfoResult() (dvdMediaInfoResult, bool, error) {
	if r == nil || strings.TrimSpace(r.tools.MediaInfoBin) == "" {
		return dvdMediaInfoResult{}, false, nil
	}
	if !looksLikeDVDSource(r.dvdProbeSource()) {
		return dvdMediaInfoResult{}, false, nil
	}
	if r.subtitleState.HasDVDMediaInfoResult {
		return r.subtitleState.DVDMediaInfoResult, true, nil
	}

	result, err := probeDVDMediaInfo(r.ctx, r.tools.MediaInfoBin, r.dvdSelectedIFOPath(), r.dvdSelectedVOBPath())
	if err != nil {
		return dvdMediaInfoResult{}, false, err
	}
	r.subtitleState.DVDMediaInfoResult = result
	r.subtitleState.HasDVDMediaInfoResult = true
	return result, true, nil
}

// probeDVDMediaInfoOnce 运行一次 mediainfo JSON 探测，并解析出字幕轨和时长信息。
func probeDVDMediaInfoOnce(ctx context.Context, mediainfoBin, path string) (dvdMediaInfoResult, error) {
	stdout, stderr, err := system.RunCommand(ctx, mediainfoBin, "--Output=JSON", path)
	if err != nil {
		return dvdMediaInfoResult{}, fmt.Errorf(system.BestErrorMessage(err, stderr, stdout))
	}
	if strings.TrimSpace(stdout) == "" {
		return dvdMediaInfoResult{}, fmt.Errorf("mediainfo returned empty output")
	}

	payloads, err := decodeMediaInfoPayloads([]byte(stdout))
	if err != nil {
		return dvdMediaInfoResult{}, err
	}

	result := dvdMediaInfoResult{
		Tracks: make([]dvdMediaInfoTrack, 0, 8),
	}

	for _, payload := range payloads {
		for _, track := range payload.Media.Track {
			trackType := strings.TrimSpace(mediaInfoTrackString(track, "@type"))
			switch strings.ToLower(trackType) {
			case "general":
				if value, ok := parseMediaInfoTrackDuration(mediaInfoTrackString(track, "Duration")); ok && value > 0 && value > result.Duration {
					result.Duration = value
				}
				if result.DisplayAspectRatio == "" {
					result.DisplayAspectRatio = normalizeMediaInfoAspectRatio(mediaInfoTrackString(track,
						"DisplayAspectRatio",
						"DisplayAspectRatio/String",
					))
				}
			case "video":
				if result.DisplayAspectRatio == "" {
					result.DisplayAspectRatio = normalizeMediaInfoAspectRatio(mediaInfoTrackString(track,
						"DisplayAspectRatio",
						"DisplayAspectRatio/String",
					))
				}
			case "text":
				streamID, _ := parseMediaInfoStreamID(mediaInfoTrackString(track, "ID"))
				result.Tracks = append(result.Tracks, dvdMediaInfoTrack{
					StreamID: streamID,
					ID:       mediaInfoTrackString(track, "ID"),
					Format:   mediaInfoTrackString(track, "Format", "Format/String"),
					Language: mediaInfoTrackString(track, "Language", "Language/String"),
					Title:    mediaInfoTrackString(track, "Title", "Title/String"),
					Source:   mediaInfoTrackString(track, "Source"),
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
