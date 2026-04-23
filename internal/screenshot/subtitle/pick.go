// Package subtitle 实现外挂/内封字幕选取与最终排序逻辑。

package subtitle

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	screenshotdvdinfo "minfo/internal/screenshot/dvdinfo"
	screenshotruntime "minfo/internal/screenshot/runtime"
	"minfo/internal/screenshot/source"
	"minfo/internal/screenshot/timestamps"
)

type internalSubtitleProbeData struct {
	rawTracks          []screenshotruntime.SubtitleTrack
	helperTracks       []screenshotruntime.BlurayHelperTrack
	helperResult       screenshotruntime.BlurayHelperResult
	blurayTracks       []screenshotruntime.SubtitleTrack
	blurayMode         string
	dvdMediaInfoTracks []screenshotruntime.DVDMediaInfoTrack
	dvdMediaInfoResult screenshotruntime.DVDMediaInfoResult
}

func (r *Runner) findExternalSubtitle() (screenshotruntime.SubtitleSelection, bool, error) {
	dir := filepath.Dir(r.SourcePath)
	base := strings.TrimSuffix(filepath.Base(r.SourcePath), filepath.Ext(r.SourcePath))

	candidates := make([]string, 0)
	for _, ext := range []string{"ass", "ssa", "srt", "vtt", "webvtt", "ttml", "dfxp", "smi", "sami", "stl", "sbv", "lrc"} {
		for _, token := range append(append(append([]string{}, langZHHansTokens...), langZHHantTokens...), langZHTokens...) {
			candidates = append(candidates,
				filepath.Join(dir, base+"."+token+"."+ext),
				filepath.Join(dir, base+"-"+token+"."+ext),
				filepath.Join(dir, base+"_"+token+"."+ext),
			)
		}
		for _, token := range langENTokens {
			candidates = append(candidates,
				filepath.Join(dir, base+"."+token+"."+ext),
				filepath.Join(dir, base+"-"+token+"."+ext),
				filepath.Join(dir, base+"_"+token+"."+ext),
			)
		}
	}

	entries, err := os.ReadDir(dir)
	if err == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			lowerName := strings.ToLower(entry.Name())
			if !strings.Contains(lowerName, strings.ToLower(base)) {
				continue
			}
			if IsKnownTextExtension(filepath.Ext(lowerName)) {
				candidates = append(candidates, filepath.Join(dir, entry.Name()))
			}
		}
	}

	bestPath := ""
	bestLang := ""
	bestScore := -1
	seen := map[string]struct{}{}
	firstUnsupportedPath := ""

	for _, candidate := range candidates {
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}

		info, err := os.Stat(candidate)
		if err != nil || info.IsDir() {
			continue
		}

		codec := CodecFromPath(candidate)
		if !IsSupportedTextCodec(codec) {
			if firstUnsupportedPath == "" {
				firstUnsupportedPath = candidate
			}
			continue
		}

		langClass := ClassifyLanguage(filepath.Base(candidate))
		if langClass == "" {
			continue
		}
		score := LanguageScore(langClass)
		if score > bestScore {
			bestScore = score
			bestPath = candidate
			bestLang = langClass
		}
	}

	if bestPath == "" {
		if firstUnsupportedPath != "" {
			return screenshotruntime.SubtitleSelection{}, false, fmt.Errorf("unsupported text subtitle codec: %s", FormatLabel(CodecFromPath(firstUnsupportedPath)))
		}
		return screenshotruntime.SubtitleSelection{}, false, nil
	}

	r.logf("[信息] 选择外挂字幕：%s （语言：%s，字幕格式：%s）", bestPath, bestLang, FormatLabel(CodecFromPath(bestPath)))
	return screenshotruntime.SubtitleSelection{
		Mode:          "external",
		File:          bestPath,
		Lang:          bestLang,
		Codec:         CodecFromPath(bestPath),
		RelativeIndex: -1,
		StreamIndex:   -1,
	}, true, nil
}

func (r *Runner) pickInternalSubtitle() (screenshotruntime.SubtitleSelection, bool, error) {
	probeData, ok, err := r.loadInternalSubtitleProbeData()
	if err != nil || !ok {
		return screenshotruntime.SubtitleSelection{}, false, err
	}

	r.logInternalSubtitleTracks(
		probeData.rawTracks,
		probeData.helperTracks,
		probeData.helperResult,
		probeData.blurayTracks,
		probeData.blurayMode,
		probeData.dvdMediaInfoTracks,
		probeData.dvdMediaInfoResult,
	)

	return r.selectInternalSubtitle(probeData)
}

func (r *Runner) loadInternalSubtitleProbeData() (internalSubtitleProbeData, bool, error) {
	data := internalSubtitleProbeData{
		helperTracks:       make([]screenshotruntime.BlurayHelperTrack, 0),
		blurayTracks:       make([]screenshotruntime.SubtitleTrack, 0),
		blurayMode:         "none",
		dvdMediaInfoTracks: make([]screenshotruntime.DVDMediaInfoTrack, 0),
	}
	state := r.state()
	currentPlaylist := state.BlurayContext.Playlist

	if source.LooksLikeDVDSource(r.dvdProbeSource()) {
		if !state.HasDVDMediaInfoResult {
			r.logProgress("字幕", 1, 3, "正在读取 DVD MediaInfo 字幕元数据。")
		}
		if result, ok := r.probeDVDMediaInfo(); ok {
			data.dvdMediaInfoResult = result
			data.dvdMediaInfoTracks = result.Tracks
			r.logf("[信息] DVD 选轨改用 mediainfo 字幕元数据：IFO=%s | VOB=%s | 字幕条数=%d",
				timestamps.DisplayProbeValue(result.ProbePath),
				timestamps.DisplayProbeValue(result.SelectedVOBPath),
				len(result.Tracks),
			)
		}
	}

	r.logProgress("字幕", 1, 3, "正在探测内封字幕轨。")
	rawTracks, err := r.probeSubtitleTracks(r.subtitleProbeSource())
	if err != nil || len(rawTracks) == 0 {
		if len(data.dvdMediaInfoTracks) > 0 {
			r.logf("[提示] ffprobe 未从 %s 识别到字幕流，但 mediainfo 已识别到 %d 条 DVD 字幕元数据。",
				r.subtitleProbeSource(),
				len(data.dvdMediaInfoTracks),
			)
		}
		return internalSubtitleProbeData{}, false, nil
	}
	data.rawTracks = rawTracks

	if state.BlurayContext.Root == "" || state.BlurayContext.Playlist == "" {
		return data, true, nil
	}

	r.logProgress("字幕", 2, 3, fmt.Sprintf("正在补充蓝光字幕元数据：playlist %s。", state.BlurayContext.Playlist))
	if result, tracks, ok := r.probeBlurayHelper(state.BlurayContext.Playlist, ""); ok {
		data.helperResult = result
		data.helperTracks = tracks
		if !HelperTracksHaveClassifiedLang(data.helperTracks) {
			for _, playlist := range r.listBlurayPlaylistsRanked() {
				if playlist == currentPlaylist {
					continue
				}
				result, altTracks, ok := r.probeBlurayHelper(playlist, "")
				if !ok || len(altTracks) == 0 {
					continue
				}
				if HelperTracksHaveClassifiedLang(altTracks) {
					state.BlurayContext.Playlist = playlist
					data.helperResult = result
					data.helperTracks = altTracks
					r.logf("[信息] 首选 playlist %s 未识别出中英字幕语言，改用候选 playlist %s。", currentPlaylist, playlist)
					break
				}
			}
		}
		data.blurayMode = "helper"
		r.logf("[信息] 原盘选轨改用 bdsub（BDInfo-style MPLS/CLPI）字幕元数据：%s / playlist %s / clip %s",
			state.BlurayContext.Root,
			state.BlurayContext.Playlist,
			state.BlurayContext.Clip,
		)
		if HelperNeedsFFprobe(rawTracks, data.helperTracks) {
			r.logProgress("字幕", 2, 3, fmt.Sprintf("正在用 ffprobe 补充蓝光字幕元数据：playlist %s。", state.BlurayContext.Playlist))
			if result, ok := r.probeBlurayFFprobe(state.BlurayContext.Playlist); ok && len(result) == len(rawTracks) && len(result) > 0 {
				data.blurayTracks = result
				data.blurayMode = "helper+ffprobe"
				r.logf("[信息] bdsub 字幕元数据不足，继续调用 ffprobe bluray playlist 补充：bluray:%s -playlist %s",
					state.BlurayContext.Root,
					state.BlurayContext.Playlist,
				)
			} else {
				r.logf("[提示] bdsub 字幕元数据不足，但 ffprobe bluray playlist 未能补充更多字幕信息。")
			}
		}
		if HelperNeedsPayloadScan(rawTracks, data.helperResult, data.helperTracks, data.blurayTracks, data.blurayMode) {
			r.logProgress("字幕", 2, 3, fmt.Sprintf("正在补充蓝光字幕 payload 元数据：playlist %s。", state.BlurayContext.Playlist))
			r.logf("[信息] 检测到同语言 PGS 候选，开始补充 payload_bytes 用于热路径密度排序：playlist %s / clip %s",
				state.BlurayContext.Playlist,
				state.BlurayContext.Clip,
			)
			if result, tracks, ok := r.probeBlurayHelper(state.BlurayContext.Playlist, "payload"); ok {
				data.helperResult = result
				data.helperTracks = tracks
			} else {
				r.logf("[提示] bdsub payload_bytes 补充失败，将继续按无密度数据的规则选轨。")
			}
		}
	}

	if data.blurayMode != "none" {
		return data, true, nil
	}

	r.logProgress("字幕", 2, 3, fmt.Sprintf("正在用 ffprobe 探测蓝光字幕：playlist %s。", state.BlurayContext.Playlist))
	if result, ok := r.probeBlurayFFprobe(state.BlurayContext.Playlist); ok && len(result) == len(rawTracks) && len(result) > 0 {
		data.blurayTracks = result
		if !TracksHaveClassifiedLang(data.blurayTracks) {
			for _, playlist := range r.listBlurayPlaylistsRanked() {
				if playlist == currentPlaylist {
					continue
				}
				altTracks, ok := r.probeBlurayFFprobe(playlist)
				if !ok || len(altTracks) != len(rawTracks) || len(altTracks) == 0 {
					continue
				}
				if TracksHaveClassifiedLang(altTracks) {
					state.BlurayContext.Playlist = playlist
					data.blurayTracks = altTracks
					r.logf("[信息] 首选 playlist %s 未识别出中英字幕语言，改用候选 playlist %s。", currentPlaylist, playlist)
					break
				}
			}
		}
		data.blurayMode = "ffprobe"
		r.logf("[信息] 原盘选轨回退到 ffprobe bluray playlist 字幕元数据：bluray:%s -playlist %s", state.BlurayContext.Root, state.BlurayContext.Playlist)
	}

	return data, true, nil
}

func (r *Runner) selectInternalSubtitle(data internalSubtitleProbeData) (screenshotruntime.SubtitleSelection, bool, error) {
	best := screenshotruntime.SubtitleTrack{}
	bestLangClass := ""
	bestRank := screenshotruntime.PreferredSubtitleRank{LangScore: -1, DispositionScore: -1}
	unsupportedBitmapDetails := make([]string, 0)
	unsupportedTextDetails := make([]string, 0)

	fallback := screenshotruntime.SubtitleTrack{}
	fallbackScore := -1

	other := screenshotruntime.SubtitleTrack{}
	otherScore := -1

	helperTrackByPID := map[int]screenshotruntime.BlurayHelperTrack{}
	for _, item := range data.helperTracks {
		helperTrackByPID[item.PID] = item
	}
	dvdTrackByStreamID := screenshotdvdinfo.ResolveTracks(data.rawTracks, data.dvdMediaInfoTracks)

	for index, track := range data.rawTracks {
		langForPick := track.Language
		titleForPick := track.Title
		pidValue, pidOK := NormalizeStreamPID(track.StreamID)
		helperMeta := screenshotruntime.BlurayHelperTrack{}
		helperMetaOK := false

		switch data.blurayMode {
		case "helper", "helper+ffprobe":
			if pidOK {
				if meta, ok := helperTrackByPID[pidValue]; ok {
					helperMeta = meta
					helperMetaOK = true
					if strings.TrimSpace(meta.Lang) != "" {
						langForPick = strings.ToLower(strings.TrimSpace(meta.Lang))
					}
				}
			}
			if !helperMetaOK && len(data.helperTracks) == len(data.rawTracks) && index < len(data.helperTracks) {
				helperMeta = data.helperTracks[index]
				helperMetaOK = true
				if strings.TrimSpace(helperMeta.Lang) != "" {
					langForPick = strings.ToLower(strings.TrimSpace(helperMeta.Lang))
				}
			}
		case "ffprobe":
			if index < len(data.blurayTracks) {
				if data.blurayTracks[index].Language != "" && data.blurayTracks[index].Language != "unknown" {
					langForPick = data.blurayTracks[index].Language
				}
				if data.blurayTracks[index].Title != "" {
					titleForPick = data.blurayTracks[index].Title
				}
			}
		}
		if (data.blurayMode == "ffprobe" || data.blurayMode == "helper+ffprobe") && index < len(data.blurayTracks) {
			needsSupplement := data.blurayMode == "ffprobe" || NeedsBluraySupplement(langForPick, titleForPick)
			if needsSupplement {
				if data.blurayTracks[index].Language != "" && data.blurayTracks[index].Language != "unknown" {
					langForPick = data.blurayTracks[index].Language
				}
				if data.blurayTracks[index].Title != "" {
					titleForPick = data.blurayTracks[index].Title
				}
			} else if strings.TrimSpace(titleForPick) == "" && data.blurayTracks[index].Title != "" {
				titleForPick = data.blurayTracks[index].Title
			}
		}

		dispositionScore := DispositionScore(track.Forced, track.IsDefault)
		if pidOK {
			if meta, ok := dvdTrackByStreamID[pidValue]; ok {
				if strings.TrimSpace(meta.Language) != "" {
					langForPick = strings.ToLower(strings.TrimSpace(meta.Language))
				}
				dispositionScore += 5
				if strings.TrimSpace(meta.Title) != "" {
					titleForPick = strings.TrimSpace(meta.Title)
				}
			}
		}

		if IsUnsupportedBitmapCodec(track.Codec) {
			unsupportedBitmapDetails = append(unsupportedBitmapDetails, fmt.Sprintf("流索引 %d(codec=%s)", track.Index, track.Codec))
			continue
		}
		if !IsSupportedTextCodec(track.Codec) && BitmapKindFromCodec(track.Codec) == screenshotruntime.BitmapSubtitleNone {
			unsupportedTextDetails = append(unsupportedTextDetails, fmt.Sprintf("流索引 %d(codec=%s)", track.Index, track.Codec))
			continue
		}

		langClass := ClassifyLanguage(strings.TrimSpace(langForPick + " " + titleForPick))
		if langClass != "" {
			rank := screenshotruntime.PreferredSubtitleRank{
				LangClass:        langClass,
				LangScore:        LanguageScore(langClass),
				DispositionScore: dispositionScore,
				PID:              pidValue,
				PIDOK:            pidOK,
				BitmapKind:       BitmapKindFromCodec(track.Codec),
				PayloadBytes:     helperMeta.PayloadBytes,
				UsePayloadBytes:  HelperHasPayloadBytes(data.helperResult) && helperMetaOK && BitmapKindFromCodec(track.Codec) == screenshotruntime.BitmapSubtitlePGS,
				Bitrate:          helperMeta.Bitrate,
				UseBitrate:       data.helperResult.BitrateScanned && helperMetaOK && BitmapKindFromCodec(track.Codec) == screenshotruntime.BitmapSubtitlePGS,
			}
			if PreferRank(rank, bestRank) {
				best = track
				best.Title = titleForPick
				bestLangClass = langClass
				bestRank = rank
			}
			continue
		}

		if track.IsDefault == 1 && dispositionScore > fallbackScore {
			fallback = track
			fallback.Title = titleForPick
			fallbackScore = dispositionScore
		}
		if dispositionScore > otherScore {
			other = track
			other.Title = titleForPick
			otherScore = dispositionScore
		}
	}

	if len(unsupportedBitmapDetails) > 0 {
		r.logf("[提示] 位图字幕目前仅支持 PGS 和 DVD Subtitle，已跳过暂不支持的位图字幕：%s", strings.Join(unsupportedBitmapDetails, ", "))
	}
	if len(unsupportedTextDetails) > 0 {
		r.logf("[提示] 已发现暂不支持的文本字幕类型：%s", strings.Join(unsupportedTextDetails, ", "))
	}

	if bestRank.LangScore >= 0 && !IsSupportedTextCodec(best.Codec) && BitmapKindFromCodec(best.Codec) == screenshotruntime.BitmapSubtitleNone {
		return screenshotruntime.SubtitleSelection{}, false, fmt.Errorf("unsupported text subtitle codec: %s", FormatLabel(best.Codec))
	}

	if bestRank.LangScore < 0 {
		if fallbackScore >= 0 {
			best = fallback
			bestLangClass = "default"
		} else if otherScore >= 0 {
			best = other
			bestLangClass = "other"
		} else if len(unsupportedTextDetails) > 0 {
			return screenshotruntime.SubtitleSelection{}, false, fmt.Errorf("unsupported text subtitle codec, only ASS/SSA/SubRip are supported")
		} else {
			return screenshotruntime.SubtitleSelection{}, false, nil
		}
	}

	relativeIndex, err := r.resolveRelativeSubtitleIndex(r.subtitleProbeSource(), best.Index)
	if err != nil {
		relativeIndex = 0
	}

	densitySuffix := internalSubtitleDensitySuffix(bestRank)
	r.logf("[信息] 选择内封字幕：流索引 %d / 字幕序号 %d （语言：%s，title：%s，default=%d，forced=%d，字幕格式：%s，codec：%s%s）",
		best.Index,
		relativeIndex,
		bestLangClass,
		timestamps.DisplayProbeValue(best.Title),
		best.IsDefault,
		best.Forced,
		FormatLabel(best.Codec),
		best.Codec,
		densitySuffix,
	)

	return screenshotruntime.SubtitleSelection{
		Mode:          "internal",
		StreamIndex:   best.Index,
		RelativeIndex: relativeIndex,
		Lang:          bestLangClass,
		Codec:         best.Codec,
		Title:         best.Title,
	}, true, nil
}

func internalSubtitleDensitySuffix(rank screenshotruntime.PreferredSubtitleRank) string {
	densitySuffix := ""
	if rank.UsePayloadBytes {
		densitySuffix = fmt.Sprintf("，payload_bytes=%d", rank.PayloadBytes)
	}
	if rank.UseBitrate {
		densitySuffix += fmt.Sprintf("，bitrate=%d", rank.Bitrate)
	}
	return densitySuffix
}
