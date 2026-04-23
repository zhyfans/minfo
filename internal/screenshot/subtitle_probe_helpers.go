// Package screenshot 提供字幕轨探测与蓝光/DVD 补充元数据辅助函数。

package screenshot

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"minfo/internal/system"
)

// prepareBlurayProbeContext 预先推导蓝光根目录、playlist 和 clip，供后续 bdsub 或 ffprobe 探测复用。
func (r *screenshotRunner) prepareBlurayProbeContext() {
	clip := strings.TrimSuffix(filepath.Base(r.sourcePath), filepath.Ext(r.sourcePath))
	if len(clip) != 5 || !allDigits(clip) {
		return
	}
	root, ok := findBlurayRootFromVideo(r.sourcePath)
	if !ok {
		return
	}
	playlists := listBlurayPlaylistsRanked(root, clip)
	if len(playlists) == 0 {
		return
	}

	r.subtitleState.BlurayContext = blurayProbeContext{
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

// listBlurayPlaylistsRanked 会列出蓝光PlaylistsRanked，并按当前规则返回排序后的结果列表。
func (r *screenshotRunner) listBlurayPlaylistsRanked() []string {
	if r.subtitleState.BlurayContext.Root == "" || r.subtitleState.BlurayContext.Clip == "" {
		return nil
	}
	playlists := listBlurayPlaylistsRanked(r.subtitleState.BlurayContext.Root, r.subtitleState.BlurayContext.Clip)
	if len(playlists) > playlistScanMax+1 {
		playlists = playlists[:playlistScanMax+1]
	}
	return playlists
}

// probeBlurayHelper 调用 bdsub 探测当前蓝光 playlist 的字幕元数据；必要时再补充 payload_bytes 或 exact bitrate。
func (r *screenshotRunner) probeBlurayHelper(playlist string, scanMode string) (blurayHelperResult, []blurayHelperTrack, bool) {
	if r.tools.BDSubBin == "" || r.subtitleState.BlurayContext.Root == "" || r.subtitleState.BlurayContext.Clip == "" {
		return blurayHelperResult{}, nil, false
	}

	args := []string{r.subtitleState.BlurayContext.Root, "--playlist", playlist, "--clip", r.subtitleState.BlurayContext.Clip}
	switch scanMode {
	case "payload":
		args = append(args, "--scan-payload")
	case "bitrate":
		args = append(args, "--scan-bitrate")
	}
	stdout, stderr, err := system.RunCommandLive(r.ctx, r.tools.BDSubBin, func(stream, line string) {
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
		return blurayHelperResult{}, nil, false
	}

	var result blurayHelperResult
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		r.logf("[提示] bdsub 输出无法解析为预期 JSON，回退 ffprobe bluray playlist 探测。")
		return blurayHelperResult{}, nil, false
	}
	if len(result.Clip.PGStreams) == 0 {
		r.logf("[提示] bdsub 返回 0 条可用 PG 流（source=%s，clip=%s，pg_stream_count=%d），回退 ffprobe bluray playlist 探测。",
			displayProbeValue(result.Source),
			displayProbeValue(result.Clip.ClipID),
			result.Clip.PGStreamCount,
		)
		return blurayHelperResult{}, nil, false
	}

	switch scanMode {
	case "payload":
		r.logf("[信息] 已调用 bdsub（metadata+payload）：%s / playlist %s / clip %s", r.subtitleState.BlurayContext.Root, playlist, r.subtitleState.BlurayContext.Clip)
	case "bitrate":
		r.logf("[信息] 已调用 bdsub（metadata+bitrate）：%s / playlist %s / clip %s", r.subtitleState.BlurayContext.Root, playlist, r.subtitleState.BlurayContext.Clip)
	default:
		r.logf("[信息] 已调用 bdsub（metadata-only）：%s / playlist %s / clip %s", r.subtitleState.BlurayContext.Root, playlist, r.subtitleState.BlurayContext.Clip)
	}
	return result, result.Clip.PGStreams, true
}

// probeBlurayFFprobe 使用 bluray: 输入和 playlist 参数补充字幕轨元数据。
func (r *screenshotRunner) probeBlurayFFprobe(playlist string) ([]subtitleTrack, bool) {
	if r.subtitleState.BlurayContext.Root == "" {
		return nil, false
	}
	tracks, err := r.probeSubtitleTracks("bluray:"+r.subtitleState.BlurayContext.Root, "-playlist", playlist)
	if err != nil || len(tracks) == 0 {
		return nil, false
	}
	return tracks, true
}

// probeDVDMediaInfo 在可用时加载 DVD 的 MediaInfo 字幕元数据。
func (r *screenshotRunner) probeDVDMediaInfo() (dvdMediaInfoResult, bool) {
	if strings.TrimSpace(r.tools.MediaInfoBin) == "" {
		return dvdMediaInfoResult{}, false
	}

	result, ok, err := r.ensureDVDMediaInfoResult()
	if !ok {
		if err != nil {
			r.logf("[提示] mediainfo(DVD) 失败：%s", err.Error())
		}
		return dvdMediaInfoResult{}, false
	}
	if len(result.Tracks) == 0 {
		r.logf("[提示] mediainfo(DVD) 未返回字幕元数据。")
		return dvdMediaInfoResult{}, false
	}

	r.logf("[信息] 已调用 mediainfo(DVD)：IFO=%s | VOB=%s",
		displayProbeValue(result.ProbePath),
		displayProbeValue(result.SelectedVOBPath),
	)
	if strings.TrimSpace(result.LanguageFallbackPath) != "" {
		r.logf("[信息] mediainfo(DVD) 语言回退：IFO 缺语言，已从 BUP 补齐：%s", result.LanguageFallbackPath)
	}
	return result, true
}

// logInternalSubtitleTracks 会把当前可见的内封字幕轨和补充元数据按统一格式写入日志。
func (r *screenshotRunner) logInternalSubtitleTracks(raw []subtitleTrack, helper []blurayHelperTrack, helperResult blurayHelperResult, bluray []subtitleTrack, blurayMode string, dvdMediaInfo []dvdMediaInfoTrack, dvdMediaInfoResult dvdMediaInfoResult) {
	if len(raw) == 0 {
		return
	}

	helperLangByPID := map[int]blurayHelperTrack{}
	for _, item := range helper {
		helperLangByPID[item.PID] = item
	}
	dvdTrackByStreamID := resolveDVDMediaInfoTracks(raw, dvdMediaInfo)

	r.logf("[信息] 可用内封字幕轨（共 %d 条）：", len(raw))
	for index, track := range raw {
		langForPick := track.Language
		titleForPick := track.Title
		tagDetails := make([]string, 0, 4)
		pidDetail := ""
		payloadDetail := ""
		bitrateDetail := ""
		helperMeta := blurayHelperTrack{}
		helperMetaOK := false

		if pid, ok := normalizeStreamPID(track.StreamID); ok {
			pidDetail = fmt.Sprintf(" | PID=%s", formatStreamPID(pid))
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
				if blurayHelperHasPayloadBytes(helperResult) {
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
					displayProbeValue(meta.ID),
					displayProbeValue(meta.Format),
					displayProbeValue(meta.Source),
				))
			}
		}
		if (blurayMode == "ffprobe" || blurayMode == "helper+ffprobe") && index < len(bluray) {
			needsSupplement := blurayMode == "ffprobe" || subtitleNeedsBluraySupplement(langForPick, titleForPick)
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

		langClass := classifySubtitleLanguage(strings.TrimSpace(langForPick + " " + titleForPick))
		if langClass == "" {
			langClass = "未识别"
		}

		r.logf("[字幕] 流索引 %d / 字幕序号 %d%s | 格式：%s | 语言：%s | title：%s | default=%d | forced=%d | codec=%s | 分类=%s | 处理=%s%s%s",
			track.Index,
			index,
			pidDetail,
			subtitleFormatLabel(track.Codec),
			displayProbeValue(langForPick),
			displayProbeValue(titleForPick),
			track.IsDefault,
			track.Forced,
			track.Codec,
			langClass,
			subtitleHandlingLabel(track.Codec),
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
			displayProbeValue(helperResult.Clip.ClipID),
			helperResult.Clip.PGStreamCount,
			blurayHelperHasPayloadBytes(helperResult),
			helperResult.BitrateScanned,
			displayProbeValue(helperResult.BitrateMode),
			helperResult.Clip.PacketSeconds,
		)
	}
	if len(dvdMediaInfo) > 0 {
		r.logf("[信息] mediainfo(DVD) 来源：IFO=%s | VOB=%s | subtitle_count=%d / duration=%s",
			displayProbeValue(dvdMediaInfoResult.ProbePath),
			displayProbeValue(dvdMediaInfoResult.SelectedVOBPath),
			len(dvdMediaInfo),
			secToHMS(dvdMediaInfoResult.Duration),
		)
	}
}

// resolveRelativeSubtitleIndex 把 ffprobe 的绝对流索引转换成 ffmpeg 需要的相对字幕序号。
func (r *screenshotRunner) resolveRelativeSubtitleIndex(input string, streamIndex int) (int, error) {
	stdout, stderr, err := system.RunCommand(r.ctx, r.tools.FFprobeBin,
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

// probeSubtitleTracks 调用 ffprobe 读取字幕轨，并归一化语言、标题和 disposition 信息。
func (r *screenshotRunner) probeSubtitleTracks(input string, extraArgs ...string) ([]subtitleTrack, error) {
	args := []string{
		"-probesize", r.settings.ProbeSize,
		"-analyzeduration", r.settings.Analyze,
		"-v", "error",
	}
	args = append(args, extraArgs...)
	args = append(args,
		"-select_streams", "s",
		"-show_entries", "stream=index,id,codec_name:stream_tags:stream_disposition=default,forced",
		"-of", "json",
		input,
	)

	stdout, stderr, err := system.RunCommand(r.ctx, r.tools.FFprobeBin, args...)
	if err != nil {
		return nil, fmt.Errorf(system.BestErrorMessage(err, stderr, stdout))
	}
	if strings.TrimSpace(stdout) == "" {
		return nil, nil
	}

	var payload ffprobeStreamsPayload
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		return nil, err
	}

	tracks := make([]subtitleTrack, 0, len(payload.Streams))
	for _, stream := range payload.Streams {
		tracks = append(tracks, subtitleTrack{
			Index:     stream.Index,
			StreamID:  jsonString(stream.ID),
			Codec:     strings.ToLower(strings.TrimSpace(stream.CodecName)),
			Language:  strings.ToLower(strings.TrimSpace(firstSubtitleLanguage(stream.Tags))),
			Title:     strings.TrimSpace(firstSubtitleTitle(stream.Tags)),
			Forced:    stream.Disposition.Forced,
			IsDefault: stream.Disposition.Default,
			Tags:      subtitleTagsSummary(stream.Tags),
		})
	}
	return tracks, nil
}
