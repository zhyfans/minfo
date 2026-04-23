// Package subtitle 提供蓝光字幕补充元数据判断辅助函数。

package subtitle

import (
	"strings"

	screenshotruntime "minfo/internal/screenshot/runtime"
)

// HelperNeedsFFprobe 判断当前 bdsub 元数据是否仍然需要 ffprobe 补充。
func HelperNeedsFFprobe(raw []screenshotruntime.SubtitleTrack, helper []screenshotruntime.BlurayHelperTrack) bool {
	if len(helper) == 0 {
		return true
	}

	helperByPID := make(map[int]screenshotruntime.BlurayHelperTrack, len(helper))
	for _, track := range helper {
		helperByPID[track.PID] = track
	}

	for index, track := range raw {
		helperMeta := screenshotruntime.BlurayHelperTrack{}
		helperMetaOK := false
		if pid, ok := NormalizeStreamPID(track.StreamID); ok {
			if meta, exists := helperByPID[pid]; exists {
				helperMeta = meta
				helperMetaOK = true
			}
		}
		if !helperMetaOK && len(helper) == len(raw) && index < len(helper) {
			helperMeta = helper[index]
			helperMetaOK = true
		}
		if !helperMetaOK {
			return true
		}
		if NeedsBluraySupplement(helperMeta.Lang, "") {
			return true
		}
	}

	return false
}

// HelperHasPayloadBytes 判断当前 bdsub 结果是否已经补充了可用于热路径排序的 payload_bytes。
func HelperHasPayloadBytes(result screenshotruntime.BlurayHelperResult) bool {
	return result.BitrateScanned || result.BitrateMode == "payload-bytes" || result.BitrateMode == "sampled-payload-bytes"
}

// HelperNeedsPayloadScan 判断当前蓝光 PGS 是否真的需要再次调用 bdsub 补充 payload_bytes。
func HelperNeedsPayloadScan(raw []screenshotruntime.SubtitleTrack, helperResult screenshotruntime.BlurayHelperResult, helper []screenshotruntime.BlurayHelperTrack, bluray []screenshotruntime.SubtitleTrack, blurayMode string) bool {
	if HelperHasPayloadBytes(helperResult) || len(helper) == 0 {
		return false
	}

	helperByPID := make(map[int]screenshotruntime.BlurayHelperTrack, len(helper))
	for _, track := range helper {
		helperByPID[track.PID] = track
	}

	langCounts := make(map[string]int, 4)
	for index, track := range raw {
		if BitmapKindFromCodec(track.Codec) != screenshotruntime.BitmapSubtitlePGS {
			continue
		}

		langForPick := track.Language
		titleForPick := track.Title
		helperMetaOK := false

		if pid, ok := NormalizeStreamPID(track.StreamID); ok {
			if meta, exists := helperByPID[pid]; exists {
				helperMetaOK = true
				if strings.TrimSpace(meta.Lang) != "" {
					langForPick = strings.ToLower(strings.TrimSpace(meta.Lang))
				}
			}
		}
		if !helperMetaOK && len(helper) == len(raw) && index < len(helper) {
			helperMetaOK = true
			if strings.TrimSpace(helper[index].Lang) != "" {
				langForPick = strings.ToLower(strings.TrimSpace(helper[index].Lang))
			}
		}
		if !helperMetaOK {
			continue
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
		}

		langClass := ClassifyLanguage(strings.TrimSpace(langForPick + " " + titleForPick))
		if langClass == "" {
			continue
		}
		langCounts[langClass]++
		if langCounts[langClass] > 1 {
			return true
		}
	}

	return false
}
