// Package subtitle 提供字幕探测输入、蓝光上下文、ffprobe 探测与日志辅助函数。

package subtitle

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	screenshotdvdinfo "minfo/internal/screenshot/dvdinfo"
	screenshotruntime "minfo/internal/screenshot/runtime"
	"minfo/internal/screenshot/source"
	"minfo/internal/screenshot/timestamps"
	"minfo/internal/system"
)

// PrepareBlurayProbeContext 预先推导蓝光根目录、playlist 和 clip，供后续 bdsub 或 ffprobe 探测复用。
func (r *Runner) PrepareBlurayProbeContext() {
	clip := strings.TrimSuffix(filepath.Base(r.SourcePath), filepath.Ext(r.SourcePath))
	if len(clip) != 5 || !allDigits(clip) {
		return
	}
	root, ok := source.FindBlurayRootFromVideo(r.SourcePath)
	if !ok {
		return
	}
	playlists := source.ListBlurayPlaylistsRanked(root, clip)
	if len(playlists) == 0 {
		return
	}

	state := r.state()
	state.BlurayContext = screenshotruntime.BlurayProbeContext{
		Root:     root,
		Playlist: playlists[0],
		Clip:     clip,
	}
	r.logf("[信息] 原盘字幕语言探测优先使用 bdsub（playlist 上下文：bluray:%s -playlist %s，来源：本地 MPLS 评分，clip：%s）",
		root,
		playlists[0],
		clip,
	)
}

func (r *Runner) listBlurayPlaylistsRanked() []string {
	state := r.state()
	if state.BlurayContext.Root == "" || state.BlurayContext.Clip == "" {
		return nil
	}
	playlists := source.ListBlurayPlaylistsRanked(state.BlurayContext.Root, state.BlurayContext.Clip)
	if len(playlists) > playlistScanMax+1 {
		playlists = playlists[:playlistScanMax+1]
	}
	return playlists
}

func (r *Runner) probeBlurayHelper(playlist string, scanMode string) (screenshotruntime.BlurayHelperResult, []screenshotruntime.BlurayHelperTrack, bool) {
	state := r.state()
	if r.Tools.BDSubBin == "" || state.BlurayContext.Root == "" || state.BlurayContext.Clip == "" {
		return screenshotruntime.BlurayHelperResult{}, nil, false
	}

	args := []string{state.BlurayContext.Root, "--playlist", playlist, "--clip", state.BlurayContext.Clip}
	switch scanMode {
	case "payload":
		args = append(args, "--scan-payload")
	case "bitrate":
		args = append(args, "--scan-bitrate")
	}
	stdout, stderr, err := system.RunCommandLive(r.Ctx, r.Tools.BDSubBin, func(stream, line string) {
		if stream != "stderr" {
			return
		}
		line = strings.TrimSpace(line)
		if line == "" {
			return
		}
		r.logf("[bdsub] %s", line)
	}, args...)
	if err != nil {
		message := strings.TrimSpace(stderr)
		if message != "" {
			r.logf("[提示] bdsub 失败：%s", message)
		}
		return screenshotruntime.BlurayHelperResult{}, nil, false
	}

	var result screenshotruntime.BlurayHelperResult
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		r.logf("[提示] bdsub 输出无法解析为预期 JSON，回退 ffprobe bluray playlist 探测。")
		return screenshotruntime.BlurayHelperResult{}, nil, false
	}
	if len(result.Clip.PGStreams) == 0 {
		r.logf("[提示] bdsub 返回 0 条可用 PG 流（source=%s，clip=%s，pg_stream_count=%d），回退 ffprobe bluray playlist 探测。",
			timestamps.DisplayProbeValue(result.Source),
			timestamps.DisplayProbeValue(result.Clip.ClipID),
			result.Clip.PGStreamCount,
		)
		return screenshotruntime.BlurayHelperResult{}, nil, false
	}

	switch scanMode {
	case "payload":
		r.logf("[信息] 已调用 bdsub（metadata+payload）：%s / playlist %s / clip %s", state.BlurayContext.Root, playlist, state.BlurayContext.Clip)
	case "bitrate":
		r.logf("[信息] 已调用 bdsub（metadata+bitrate）：%s / playlist %s / clip %s", state.BlurayContext.Root, playlist, state.BlurayContext.Clip)
	default:
		r.logf("[信息] 已调用 bdsub（metadata-only）：%s / playlist %s / clip %s", state.BlurayContext.Root, playlist, state.BlurayContext.Clip)
	}
	return result, result.Clip.PGStreams, true
}

func (r *Runner) probeBlurayFFprobe(playlist string) ([]screenshotruntime.SubtitleTrack, bool) {
	state := r.state()
	if state.BlurayContext.Root == "" {
		return nil, false
	}
	tracks, err := r.probeSubtitleTracks("bluray:"+state.BlurayContext.Root, "-playlist", playlist)
	if err != nil || len(tracks) == 0 {
		return nil, false
	}
	return tracks, true
}

func (r *Runner) probeDVDMediaInfo() (screenshotruntime.DVDMediaInfoResult, bool) {
	if strings.TrimSpace(r.Tools.MediaInfoBin) == "" {
		return screenshotruntime.DVDMediaInfoResult{}, false
	}

	result, ok, err := r.ensureDVDMediaInfoResult()
	if !ok {
		if err != nil {
			r.logf("[提示] mediainfo(DVD) 失败：%s", err.Error())
		}
		return screenshotruntime.DVDMediaInfoResult{}, false
	}
	if len(result.Tracks) == 0 {
		r.logf("[提示] mediainfo(DVD) 未返回字幕元数据。")
		return screenshotruntime.DVDMediaInfoResult{}, false
	}

	r.logf("[信息] 已调用 mediainfo(DVD)：IFO=%s | VOB=%s",
		timestamps.DisplayProbeValue(result.ProbePath),
		timestamps.DisplayProbeValue(result.SelectedVOBPath),
	)
	if strings.TrimSpace(result.LanguageFallbackPath) != "" {
		r.logf("[信息] mediainfo(DVD) 语言回退：IFO 缺语言，已从 BUP 补齐：%s", result.LanguageFallbackPath)
	}
	return result, true
}

func (r *Runner) resolveRelativeSubtitleIndex(input string, streamIndex int) (int, error) {
	stdout, stderr, err := system.RunCommand(r.Ctx, r.Tools.FFprobeBin,
		"-v", "error",
		"-select_streams", "s",
		"-show_entries", "stream=index",
		"-of", "csv=p=0",
		input,
	)
	if err != nil {
		return 0, fmt.Errorf(system.BestErrorMessage(err, stderr, stdout))
	}

	lines := strings.Split(stdout, "\n")
	relative := 0
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		value, convErr := strconv.Atoi(line)
		if convErr != nil {
			continue
		}
		if value == streamIndex {
			return relative, nil
		}
		relative++
	}
	return 0, errors.New("subtitle stream not found in ffprobe select_streams output")
}

func (r *Runner) probeSubtitleTracks(input string, extraArgs ...string) ([]screenshotruntime.SubtitleTrack, error) {
	args := []string{
		"-probesize", r.Settings.ProbeSize,
		"-analyzeduration", r.Settings.Analyze,
		"-v", "error",
	}
	args = append(args, extraArgs...)
	args = append(args,
		"-select_streams", "s",
		"-show_entries", "stream=index,id,codec_name:stream_tags:stream_disposition=default,forced",
		"-of", "json",
		input,
	)

	stdout, stderr, err := system.RunCommand(r.Ctx, r.Tools.FFprobeBin, args...)
	if err != nil {
		return nil, fmt.Errorf(system.BestErrorMessage(err, stderr, stdout))
	}
	if strings.TrimSpace(stdout) == "" {
		return nil, nil
	}

	var payload screenshotruntime.FFprobeStreamsPayload
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		return nil, err
	}

	tracks := make([]screenshotruntime.SubtitleTrack, 0, len(payload.Streams))
	for _, stream := range payload.Streams {
		tracks = append(tracks, screenshotruntime.SubtitleTrack{
			Index:     stream.Index,
			StreamID:  JSONString(stream.ID),
			Codec:     strings.ToLower(strings.TrimSpace(stream.CodecName)),
			Language:  strings.ToLower(strings.TrimSpace(FirstLanguage(stream.Tags))),
			Title:     strings.TrimSpace(FirstTitle(stream.Tags)),
			Forced:    stream.Disposition.Forced,
			IsDefault: stream.Disposition.Default,
			Tags:      TagsSummary(stream.Tags),
		})
	}
	return tracks, nil
}

func (r *Runner) logInternalSubtitleTracks(raw []screenshotruntime.SubtitleTrack, helper []screenshotruntime.BlurayHelperTrack, helperResult screenshotruntime.BlurayHelperResult, bluray []screenshotruntime.SubtitleTrack, blurayMode string, dvdMediaInfo []screenshotruntime.DVDMediaInfoTrack, dvdMediaInfoResult screenshotruntime.DVDMediaInfoResult) {
	if len(raw) == 0 {
		return
	}

	helperLangByPID := map[int]screenshotruntime.BlurayHelperTrack{}
	for _, item := range helper {
		helperLangByPID[item.PID] = item
	}
	dvdTrackByStreamID := screenshotdvdinfo.ResolveTracks(raw, dvdMediaInfo)

	r.logf("[信息] 可用内封字幕轨（共 %d 条）：", len(raw))
	for index, track := range raw {
		langForPick := track.Language
		titleForPick := track.Title
		tagDetails := make([]string, 0, 4)
		pidDetail := ""
		payloadDetail := ""
		bitrateDetail := ""
		helperMeta := screenshotruntime.BlurayHelperTrack{}
		helperMetaOK := false

		if pid, ok := NormalizeStreamPID(track.StreamID); ok {
			pidDetail = fmt.Sprintf(" | PID=%s", FormatStreamPID(pid))
			if blurayMode == "helper" || blurayMode == "helper+ffprobe" {
				if meta, ok := helperLangByPID[pid]; ok {
					helperMeta = meta
					helperMetaOK = true
					if strings.TrimSpace(meta.Lang) != "" {
						langForPick = strings.ToLower(strings.TrimSpace(meta.Lang))
					}
				}
			}
			if !helperMetaOK && len(helper) == len(raw) && index < len(helper) && (blurayMode == "helper" || blurayMode == "helper+ffprobe") {
				helperMeta = helper[index]
				helperMetaOK = true
				if strings.TrimSpace(helperMeta.Lang) != "" {
					langForPick = strings.ToLower(strings.TrimSpace(helperMeta.Lang))
				}
			}
			if helperMetaOK {
				bdsubTag := fmt.Sprintf("bdsub: coding_type=%d, char_code=%d, subpath_id=%d", helperMeta.CodingType, helperMeta.CharCode, helperMeta.SubpathID)
				if HelperHasPayloadBytes(helperResult) {
					payloadDetail = fmt.Sprintf(" | payload_bytes=%d", helperMeta.PayloadBytes)
					bdsubTag += fmt.Sprintf(", payload_bytes=%d", helperMeta.PayloadBytes)
				}
				if helperResult.BitrateScanned {
					bitrateDetail = fmt.Sprintf(" | bitrate=%d", helperMeta.Bitrate)
					bdsubTag += fmt.Sprintf(", bitrate=%d", helperMeta.Bitrate)
				}
				tagDetails = append(tagDetails, bdsubTag)
			}
			if meta, ok := dvdTrackByStreamID[pid]; ok {
				if strings.TrimSpace(meta.Language) != "" {
					langForPick = strings.ToLower(strings.TrimSpace(meta.Language))
				}
				if strings.TrimSpace(meta.Title) != "" {
					titleForPick = strings.TrimSpace(meta.Title)
				}
				tagDetails = append(tagDetails, fmt.Sprintf("mediainfo: id=%s, format=%s, source=%s",
					timestamps.DisplayProbeValue(meta.ID),
					timestamps.DisplayProbeValue(meta.Format),
					timestamps.DisplayProbeValue(meta.Source),
				))
			}
		}
		if (blurayMode == "ffprobe" || blurayMode == "helper+ffprobe") && index < len(bluray) {
			needsSupplement := blurayMode == "ffprobe" || NeedsBluraySupplement(langForPick, titleForPick)
			if needsSupplement {
				if bluray[index].Language != "" && bluray[index].Language != "unknown" {
					langForPick = bluray[index].Language
				}
				if bluray[index].Title != "" {
					titleForPick = bluray[index].Title
				}
			} else if strings.TrimSpace(titleForPick) == "" && bluray[index].Title != "" {
				titleForPick = bluray[index].Title
			}
			if bluray[index].Tags != "" {
				tagDetails = append(tagDetails, "ffprobe(bluray) tags: "+bluray[index].Tags)
			}
		}

		langClass := ClassifyLanguage(strings.TrimSpace(langForPick + " " + titleForPick))
		if langClass == "" {
			langClass = "未识别"
		}

		r.logf("[字幕] 流索引 %d / 字幕序号 %d%s | 格式：%s | 语言：%s | title：%s | default=%d | forced=%d | codec=%s | 分类=%s | 处理=%s%s%s",
			track.Index,
			index,
			pidDetail,
			FormatLabel(track.Codec),
			timestamps.DisplayProbeValue(langForPick),
			timestamps.DisplayProbeValue(titleForPick),
			track.IsDefault,
			track.Forced,
			track.Codec,
			langClass,
			HandlingLabel(track.Codec),
			payloadDetail,
			bitrateDetail,
		)

		if langClass == "未识别" {
			details := make([]string, 0, 3)
			if track.Tags != "" {
				details = append(details, "ffprobe(file) tags: "+track.Tags)
			}
			if len(tagDetails) > 0 {
				details = append(details, tagDetails...)
			}
			if len(details) > 0 {
				r.logf("[字幕] 流索引 %d 标签：%s", track.Index, strings.Join(details, " | "))
			}
		}
	}

	if (blurayMode == "helper" || blurayMode == "helper+ffprobe") && helperResult.Source != "" {
		r.logf("[信息] bdsub 来源：%s / clip=%s / pg_stream_count=%d / payload_ready=%t / bitrate_scanned=%t / bitrate_mode=%s / packet_seconds=%.3f",
			helperResult.Source,
			timestamps.DisplayProbeValue(helperResult.Clip.ClipID),
			helperResult.Clip.PGStreamCount,
			HelperHasPayloadBytes(helperResult),
			helperResult.BitrateScanned,
			timestamps.DisplayProbeValue(helperResult.BitrateMode),
			helperResult.Clip.PacketSeconds,
		)
	}
	if len(dvdMediaInfo) > 0 {
		r.logf("[信息] mediainfo(DVD) 来源：IFO=%s | VOB=%s | subtitle_count=%d / duration=%s",
			timestamps.DisplayProbeValue(dvdMediaInfoResult.ProbePath),
			timestamps.DisplayProbeValue(dvdMediaInfoResult.SelectedVOBPath),
			len(dvdMediaInfo),
			timestamps.SecToHMS(dvdMediaInfoResult.Duration),
		)
	}
}
