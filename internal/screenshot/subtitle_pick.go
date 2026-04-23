// Package screenshot 实现外挂与内封字幕的选轨逻辑。

package screenshot

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// findExternalSubtitle 会在视频附近查找语言优先级最高的外挂字幕文件。
func (r *screenshotRunner) findExternalSubtitle() (subtitleSelection, bool, error) {
	dir := filepath.Dir(r.sourcePath)
	base := strings.TrimSuffix(filepath.Base(r.sourcePath), filepath.Ext(r.sourcePath))

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
			if isKnownTextSubtitleExtension(filepath.Ext(lowerName)) {
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

		codec := subtitleCodecFromPath(candidate)
		if !isSupportedTextSubtitleCodec(codec) {
			if firstUnsupportedPath == "" {
				firstUnsupportedPath = candidate
			}
			continue
		}

		langClass := classifySubtitleLanguage(filepath.Base(candidate))
		if langClass == "" {
			continue
		}
		score := subtitleLanguageScore(langClass)
		if score > bestScore {
			bestScore = score
			bestPath = candidate
			bestLang = langClass
		}
	}

	if bestPath == "" {
		if firstUnsupportedPath != "" {
			return subtitleSelection{}, false, fmt.Errorf("unsupported text subtitle codec: %s", subtitleFormatLabel(subtitleCodecFromPath(firstUnsupportedPath)))
		}
		return subtitleSelection{}, false, nil
	}

	r.logf("[信息] 选择外挂字幕：%s （语言：%s，字幕格式：%s）", bestPath, bestLang, subtitleFormatLabel(subtitleCodecFromPath(bestPath)))
	return subtitleSelection{
		Mode:          "external",
		File:          bestPath,
		Lang:          bestLang,
		Codec:         subtitleCodecFromPath(bestPath),
		RelativeIndex: -1,
		StreamIndex:   -1,
	}, true, nil
}

// pickInternalSubtitle 会综合语言、默认标记、PID 和原盘补充信息选择最合适的内封字幕轨。
func (r *screenshotRunner) pickInternalSubtitle() (subtitleSelection, bool, error) {
	helperTracks := make([]blurayHelperTrack, 0)
	helperResult := blurayHelperResult{}
	blurayTracks := make([]subtitleTrack, 0)
	blurayMode := "none"
	dvdMediaInfoTracks := make([]dvdMediaInfoTrack, 0)
	dvdMediaInfoResult := dvdMediaInfoResult{}
	currentPlaylist := r.subtitleState.BlurayContext.Playlist

	if looksLikeDVDSource(r.dvdProbeSource()) {
		if !r.subtitleState.HasDVDMediaInfoResult {
			r.logProgress("字幕", 1, 3, "正在读取 DVD MediaInfo 字幕元数据。")
		}
		if result, ok := r.probeDVDMediaInfo(); ok {
			dvdMediaInfoResult = result
			dvdMediaInfoTracks = result.Tracks
			r.logf("[信息] DVD 选轨改用 mediainfo 字幕元数据：IFO=%s | VOB=%s | 字幕条数=%d",
				displayProbeValue(result.ProbePath),
				displayProbeValue(result.SelectedVOBPath),
				len(result.Tracks),
			)
		}
	}

	r.logProgress("字幕", 1, 3, "正在探测内封字幕轨。")
	rawTracks, err := r.probeSubtitleTracks(r.subtitleProbeSource())
	if err != nil || len(rawTracks) == 0 {
		if len(dvdMediaInfoTracks) > 0 {
			r.logf("[提示] ffprobe 未从 %s 识别到字幕流，但 mediainfo 已识别到 %d 条 DVD 字幕元数据。",
				r.subtitleProbeSource(),
				len(dvdMediaInfoTracks),
			)
		}
		return subtitleSelection{}, false, nil
	}

	if r.subtitleState.BlurayContext.Root != "" && r.subtitleState.BlurayContext.Playlist != "" {
		r.logProgress("字幕", 2, 3, fmt.Sprintf("正在补充蓝光字幕元数据：playlist %s。", r.subtitleState.BlurayContext.Playlist))
		if result, tracks, ok := r.probeBlurayHelper(r.subtitleState.BlurayContext.Playlist, ""); ok {
			helperResult = result
			helperTracks = tracks
			if !helperTracksHaveClassifiedLang(helperTracks) {
				for _, playlist := range r.listBlurayPlaylistsRanked() {
					if playlist == currentPlaylist {
						continue
					}
					result, altTracks, ok := r.probeBlurayHelper(playlist, "")
					if !ok || len(altTracks) == 0 {
						continue
					}
					if helperTracksHaveClassifiedLang(altTracks) {
						r.subtitleState.BlurayContext.Playlist = playlist
						helperResult = result
						helperTracks = altTracks
						r.logf("[信息] 首选 playlist %s 未识别出中英字幕语言，改用候选 playlist %s。", currentPlaylist, playlist)
						break
					}
				}
			}
			blurayMode = "helper"
			r.logf("[信息] 原盘选轨改用 bdsub（BDInfo-style MPLS/CLPI）字幕元数据：%s / playlist %s / clip %s",
				r.subtitleState.BlurayContext.Root,
				r.subtitleState.BlurayContext.Playlist,
				r.subtitleState.BlurayContext.Clip,
			)
			if blurayHelperNeedsFFprobe(rawTracks, helperTracks) {
				r.logProgress("字幕", 2, 3, fmt.Sprintf("正在用 ffprobe 补充蓝光字幕元数据：playlist %s。", r.subtitleState.BlurayContext.Playlist))
				if result, ok := r.probeBlurayFFprobe(r.subtitleState.BlurayContext.Playlist); ok && len(result) == len(rawTracks) && len(result) > 0 {
					blurayTracks = result
					blurayMode = "helper+ffprobe"
					r.logf("[信息] bdsub 字幕元数据不足，继续调用 ffprobe bluray playlist 补充：bluray:%s -playlist %s",
						r.subtitleState.BlurayContext.Root,
						r.subtitleState.BlurayContext.Playlist,
					)
				} else {
					r.logf("[提示] bdsub 字幕元数据不足，但 ffprobe bluray playlist 未能补充更多字幕信息。")
				}
			}
			if blurayHelperNeedsPayloadScan(rawTracks, helperResult, helperTracks, blurayTracks, blurayMode) {
				r.logProgress("字幕", 2, 3, fmt.Sprintf("正在补充蓝光字幕 payload 元数据：playlist %s。", r.subtitleState.BlurayContext.Playlist))
				r.logf("[信息] 检测到同语言 PGS 候选，开始补充 payload_bytes 用于热路径密度排序：playlist %s / clip %s",
					r.subtitleState.BlurayContext.Playlist,
					r.subtitleState.BlurayContext.Clip,
				)
				if result, tracks, ok := r.probeBlurayHelper(r.subtitleState.BlurayContext.Playlist, "payload"); ok {
					helperResult = result
					helperTracks = tracks
				} else {
					r.logf("[提示] bdsub payload_bytes 补充失败，将继续按无密度数据的规则选轨。")
				}
			}
		}

		if blurayMode == "none" {
			r.logProgress("字幕", 2, 3, fmt.Sprintf("正在用 ffprobe 探测蓝光字幕：playlist %s。", r.subtitleState.BlurayContext.Playlist))
			if result, ok := r.probeBlurayFFprobe(r.subtitleState.BlurayContext.Playlist); ok && len(result) == len(rawTracks) && len(result) > 0 {
				blurayTracks = result
				if !tracksHaveClassifiedLang(blurayTracks) {
					for _, playlist := range r.listBlurayPlaylistsRanked() {
						if playlist == currentPlaylist {
							continue
						}
						altTracks, ok := r.probeBlurayFFprobe(playlist)
						if !ok || len(altTracks) != len(rawTracks) || len(altTracks) == 0 {
							continue
						}
						if tracksHaveClassifiedLang(altTracks) {
							r.subtitleState.BlurayContext.Playlist = playlist
							blurayTracks = altTracks
							r.logf("[信息] 首选 playlist %s 未识别出中英字幕语言，改用候选 playlist %s。", currentPlaylist, playlist)
							break
						}
					}
				}
				blurayMode = "ffprobe"
				r.logf("[信息] 原盘选轨回退到 ffprobe bluray playlist 字幕元数据：bluray:%s -playlist %s", r.subtitleState.BlurayContext.Root, r.subtitleState.BlurayContext.Playlist)
			}
		}
	}

	r.logInternalSubtitleTracks(rawTracks, helperTracks, helperResult, blurayTracks, blurayMode, dvdMediaInfoTracks, dvdMediaInfoResult)

	best := subtitleTrack{}
	bestLangClass := ""
	bestRank := preferredSubtitleRank{LangScore: -1, DispositionScore: -1}
	unsupportedBitmapDetails := make([]string, 0)
	unsupportedTextDetails := make([]string, 0)

	fallback := subtitleTrack{}
	fallbackScore := -1

	other := subtitleTrack{}
	otherScore := -1

	helperTrackByPID := map[int]blurayHelperTrack{}
	for _, item := range helperTracks {
		helperTrackByPID[item.PID] = item
	}
	dvdTrackByStreamID := resolveDVDMediaInfoTracks(rawTracks, dvdMediaInfoTracks)

	for index, track := range rawTracks {
		langForPick := track.Language
		titleForPick := track.Title
		pidValue, pidOK := normalizeStreamPID(track.StreamID)
		helperMeta := blurayHelperTrack{}
		helperMetaOK := false

		switch blurayMode {
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
			if !helperMetaOK && len(helperTracks) == len(rawTracks) && index < len(helperTracks) {
				helperMeta = helperTracks[index]
				helperMetaOK = true
				if strings.TrimSpace(helperMeta.Lang) != "" {
					langForPick = strings.ToLower(strings.TrimSpace(helperMeta.Lang))
				}
			}
		case "ffprobe":
			if index < len(blurayTracks) {
				if blurayTracks[index].Language != "" && blurayTracks[index].Language != "unknown" {
					langForPick = blurayTracks[index].Language
				}
				if blurayTracks[index].Title != "" {
					titleForPick = blurayTracks[index].Title
				}
			}
		}
		if (blurayMode == "ffprobe" || blurayMode == "helper+ffprobe") && index < len(blurayTracks) {
			needsSupplement := blurayMode == "ffprobe" || subtitleNeedsBluraySupplement(langForPick, titleForPick)
			if needsSupplement {
				if blurayTracks[index].Language != "" && blurayTracks[index].Language != "unknown" {
					langForPick = blurayTracks[index].Language
				}
				if blurayTracks[index].Title != "" {
					titleForPick = blurayTracks[index].Title
				}
			} else if strings.TrimSpace(titleForPick) == "" && blurayTracks[index].Title != "" {
				titleForPick = blurayTracks[index].Title
			}
		}

		dispositionScore := subtitleDispositionScore(track.Forced, track.IsDefault)
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

		if isUnsupportedBitmapSubtitleCodec(track.Codec) {
			unsupportedBitmapDetails = append(unsupportedBitmapDetails, fmt.Sprintf("流索引 %d(codec=%s)", track.Index, track.Codec))
			continue
		}
		if !isSupportedTextSubtitleCodec(track.Codec) && bitmapSubtitleKindFromCodec(track.Codec) == bitmapSubtitleNone {
			unsupportedTextDetails = append(unsupportedTextDetails, fmt.Sprintf("流索引 %d(codec=%s)", track.Index, track.Codec))
			continue
		}

		langClass := classifySubtitleLanguage(strings.TrimSpace(langForPick + " " + titleForPick))

		if langClass != "" {
			rank := preferredSubtitleRank{
				LangClass:        langClass,
				LangScore:        subtitleLanguageScore(langClass),
				DispositionScore: dispositionScore,
				PID:              pidValue,
				PIDOK:            pidOK,
				BitmapKind:       bitmapSubtitleKindFromCodec(track.Codec),
				PayloadBytes:     helperMeta.PayloadBytes,
				UsePayloadBytes:  blurayHelperHasPayloadBytes(helperResult) && helperMetaOK && bitmapSubtitleKindFromCodec(track.Codec) == bitmapSubtitlePGS,
				Bitrate:          helperMeta.Bitrate,
				UseBitrate:       helperResult.BitrateScanned && helperMetaOK && bitmapSubtitleKindFromCodec(track.Codec) == bitmapSubtitlePGS,
			}
			if preferPreferredSubtitleRank(rank, bestRank) {
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

	if bestRank.LangScore >= 0 && !isSupportedTextSubtitleCodec(best.Codec) && bitmapSubtitleKindFromCodec(best.Codec) == bitmapSubtitleNone {
		return subtitleSelection{}, false, fmt.Errorf("unsupported text subtitle codec: %s", subtitleFormatLabel(best.Codec))
	}

	if bestRank.LangScore < 0 {
		if fallbackScore >= 0 {
			best = fallback
			bestLangClass = "default"
		} else if otherScore >= 0 {
			best = other
			bestLangClass = "other"
		} else if len(unsupportedTextDetails) > 0 {
			return subtitleSelection{}, false, fmt.Errorf("unsupported text subtitle codec, only ASS/SSA/SubRip are supported")
		} else {
			return subtitleSelection{}, false, nil
		}
	}

	relativeIndex, err := r.resolveRelativeSubtitleIndex(r.subtitleProbeSource(), best.Index)
	if err != nil {
		relativeIndex = 0
	}

	densitySuffix := ""
	if bestRank.UsePayloadBytes {
		densitySuffix = fmt.Sprintf("，payload_bytes=%d", bestRank.PayloadBytes)
	}
	if bestRank.UseBitrate {
		densitySuffix += fmt.Sprintf("，bitrate=%d", bestRank.Bitrate)
	}

	r.logf("[信息] 选择内封字幕：流索引 %d / 字幕序号 %d （语言：%s，title：%s，default=%d，forced=%d，字幕格式：%s，codec：%s%s）",
		best.Index,
		relativeIndex,
		bestLangClass,
		displayProbeValue(best.Title),
		best.IsDefault,
		best.Forced,
		subtitleFormatLabel(best.Codec),
		best.Codec,
		densitySuffix,
	)

	return subtitleSelection{
		Mode:          "internal",
		StreamIndex:   best.Index,
		RelativeIndex: relativeIndex,
		Lang:          bestLangClass,
		Codec:         best.Codec,
		Title:         best.Title,
	}, true, nil
}

// logSubtitleFallback 会记录字幕回退，方便排查当前执行过程中的关键状态。
func (r *screenshotRunner) logSubtitleFallback(modeLabel string) {
	switch r.subtitle.Lang {
	case "zh-Hant":
		r.logf("[提示] 未找到简体中文字幕，改用繁体%s字幕。", modeLabel)
	case "zh":
		r.logf("[提示] 检测到中文字幕，但未明确识别简繁体，使用中文%s字幕。", modeLabel)
	case "en":
		r.logf("[提示] 未找到中文字幕，改用英文%s字幕。", modeLabel)
	case "other":
		r.logf("[提示] 未找到简体/繁体/英文字幕，改用其他%s字幕。", modeLabel)
	case "default":
		r.logf("[提示] 未找到简体/繁体/英文字幕，改用默认%s字幕。", modeLabel)
	}
}

// logSelectedSubtitleSummary 记录最终选中的字幕来源、格式和渲染方式。
func (r *screenshotRunner) logSelectedSubtitleSummary() {
	if r.subtitle.Mode == "none" {
		return
	}

	source := "外挂"
	render := "直接使用外挂文件"
	if r.subtitle.ExtractedText {
		source = "内封"
		render = "提取内封文字字幕后按外挂文件渲染"
	} else if r.subtitle.Mode == "internal" {
		source = "内封"
		render = "直接使用内封轨道"
	}
	if strings.TrimSpace(r.subtitleState.SubtitleFontDir) != "" {
		render += "（优先使用 MKV 附件字体）"
	}

	r.logf("[字幕格式] 来源：%s | 格式：%s | 渲染：%s", source, subtitleFormatLabel(r.subtitle.Codec), render)
}

// subtitleProbeSource 返回 ffprobe 探测字幕轨时使用的输入路径。
func (r *screenshotRunner) subtitleProbeSource() string {
	return r.sourcePath
}

// dvdMediaInfoSource 返回 DVD MediaInfo 探测优先使用的输入路径。
func (r *screenshotRunner) dvdMediaInfoSource() string {
	if strings.TrimSpace(r.dvdMediaInfoPath) != "" {
		return r.dvdMediaInfoPath
	}
	return r.dvdProbeSource()
}

// dvdSelectedIFOPath 返回当前 DVD 选择对应的最佳 IFO 路径。
func (r *screenshotRunner) dvdSelectedIFOPath() string {
	resolved, ok := dvdMediaInfoIFOPath(r.dvdProbeSource())
	if ok {
		return resolved
	}
	resolved, ok = dvdMediaInfoIFOPath(r.dvdMediaInfoSource())
	if ok {
		return resolved
	}
	return r.dvdMediaInfoSource()
}

// dvdSelectedVOBPath 返回当前 DVD 选择对应的最佳标题 VOB 路径。
func (r *screenshotRunner) dvdSelectedVOBPath() string {
	resolved, ok := dvdMediaInfoTitleVOBPath(r.dvdProbeSource())
	if ok {
		return resolved
	}
	resolved, ok = dvdMediaInfoTitleVOBPath(r.dvdMediaInfoSource())
	if ok {
		return resolved
	}
	return ""
}

// dvdProbeSource 返回 DVD 字幕探测时使用的基础输入路径。
func (r *screenshotRunner) dvdProbeSource() string {
	return r.sourcePath
}
