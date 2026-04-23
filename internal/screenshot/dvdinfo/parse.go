// Package dvdinfo 提供 DVD mediainfo 结果解析、语言回退和轨道映射逻辑。

package dvdinfo

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"

	screenshotruntime "minfo/internal/screenshot/runtime"
)

// mediaInfoPayload 描述 mediainfo JSON 中单个 media 节点的原始结构。
type mediaInfoPayload struct {
	Media struct {
		Track []map[string]interface{} `json:"track"`
	} `json:"media"`
}

var mediaInfoHexIDPattern = regexp.MustCompile(`(?i)\(0x([0-9a-f]+)\)`)

// NeedsLanguageFallback 判断结果中是否仍有字幕轨缺少可用语言信息。
func NeedsLanguageFallback(result screenshotruntime.DVDMediaInfoResult) bool {
	if len(result.Tracks) == 0 {
		return false
	}
	for _, track := range result.Tracks {
		if !hasLanguage(track.Language) {
			return true
		}
	}
	return false
}

// MergeLanguageFallback 会合并 DVD mediainfo 语言回退，并保留有效信息。
func MergeLanguageFallback(primary, fallback screenshotruntime.DVDMediaInfoResult) (screenshotruntime.DVDMediaInfoResult, bool) {
	if len(primary.Tracks) == 0 || len(fallback.Tracks) == 0 {
		return primary, false
	}

	merged := primary
	merged.Tracks = append([]screenshotruntime.DVDMediaInfoTrack(nil), primary.Tracks...)

	fallbackByStreamID := make(map[int][]screenshotruntime.DVDMediaInfoTrack, len(fallback.Tracks))
	fallbackIndexByStreamID := make(map[int][]int, len(fallback.Tracks))
	fallbackOrdered := make([]screenshotruntime.DVDMediaInfoTrack, 0, len(fallback.Tracks))
	for _, track := range fallback.Tracks {
		if !hasLanguage(track.Language) {
			continue
		}
		fallbackOrdered = append(fallbackOrdered, track)
		orderedIndex := len(fallbackOrdered) - 1
		if track.StreamID > 0 {
			fallbackByStreamID[track.StreamID] = append(fallbackByStreamID[track.StreamID], track)
			fallbackIndexByStreamID[track.StreamID] = append(fallbackIndexByStreamID[track.StreamID], orderedIndex)
		}
	}
	if len(fallbackOrdered) == 0 {
		return primary, false
	}

	usedOrdered := make([]bool, len(fallbackOrdered))
	used := false
	for index := range merged.Tracks {
		if hasLanguage(merged.Tracks[index].Language) {
			continue
		}

		if mergedTrack, ok := fillLanguageByStreamID(merged.Tracks[index], fallbackByStreamID, fallbackIndexByStreamID, usedOrdered); ok {
			merged.Tracks[index] = mergedTrack
			used = true
			continue
		}

		for fallbackIndex, candidate := range fallbackOrdered {
			if usedOrdered[fallbackIndex] {
				continue
			}
			merged.Tracks[index] = mergeTrack(merged.Tracks[index], candidate)
			usedOrdered[fallbackIndex] = true
			used = true
			break
		}
	}

	if merged.Duration <= 0 && fallback.Duration > 0 {
		merged.Duration = fallback.Duration
	}
	return merged, used
}

func fillLanguageByStreamID(track screenshotruntime.DVDMediaInfoTrack, fallbackByStreamID map[int][]screenshotruntime.DVDMediaInfoTrack, fallbackIndexByStreamID map[int][]int, usedOrdered []bool) (screenshotruntime.DVDMediaInfoTrack, bool) {
	if track.StreamID <= 0 {
		return track, false
	}
	candidates := fallbackByStreamID[track.StreamID]
	candidateIndexes := fallbackIndexByStreamID[track.StreamID]
	if len(candidates) == 0 || len(candidateIndexes) == 0 {
		return track, false
	}
	for index, candidate := range candidates {
		if index >= len(candidateIndexes) {
			break
		}
		orderedIndex := candidateIndexes[index]
		if orderedIndex < 0 || orderedIndex >= len(usedOrdered) || usedOrdered[orderedIndex] {
			continue
		}
		usedOrdered[orderedIndex] = true
		return mergeTrack(track, candidate), true
	}
	return track, false
}

func mergeTrack(track, fallback screenshotruntime.DVDMediaInfoTrack) screenshotruntime.DVDMediaInfoTrack {
	if !hasLanguage(track.Language) && hasLanguage(fallback.Language) {
		track.Language = fallback.Language
		track.Source = fallback.Source
	}
	if strings.TrimSpace(track.Title) == "" && strings.TrimSpace(fallback.Title) != "" {
		track.Title = strings.TrimSpace(fallback.Title)
	}
	if strings.TrimSpace(track.Format) == "" && strings.TrimSpace(fallback.Format) != "" {
		track.Format = strings.TrimSpace(fallback.Format)
	}
	return track
}

func hasLanguage(language string) bool {
	switch strings.ToLower(strings.TrimSpace(language)) {
	case "", "unknown", "und", "undefined", "null", "n/a", "na":
		return false
	default:
		return true
	}
}

func decodePayloads(data []byte) ([]mediaInfoPayload, error) {
	var single mediaInfoPayload
	if err := json.Unmarshal(data, &single); err == nil {
		return []mediaInfoPayload{single}, nil
	}

	var multiple []mediaInfoPayload
	if err := json.Unmarshal(data, &multiple); err == nil {
		return multiple, nil
	}

	return nil, fmt.Errorf("unsupported mediainfo JSON shape")
}

func trackString(track map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		value := strings.TrimSpace(jsonString(track[key]))
		if value != "" {
			return value
		}
	}
	return ""
}

func parseStreamID(raw string) (int, bool) {
	matches := mediaInfoHexIDPattern.FindAllStringSubmatch(raw, -1)
	if len(matches) > 0 {
		last := matches[len(matches)-1]
		if len(last) >= 2 {
			value, err := strconv.ParseInt(last[1], 16, 64)
			if err == nil {
				return int(value), true
			}
		}
	}

	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false
	}
	if value, err := strconv.Atoi(raw); err == nil {
		return value, true
	}
	return 0, false
}

func parseTrackDuration(raw string) (float64, bool) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return 0, false
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil || parsed <= 0 {
		return 0, false
	}
	return parsed / 1000.0, true
}

// NormalizeAspectRatio 把 mediainfo 的显示宽高比统一转换成更稳定的 ratio 字符串。
func NormalizeAspectRatio(raw string) string { return normalizeAspectRatio(raw) }

func normalizeAspectRatio(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}
	if strings.Contains(value, ":") {
		return value
	}
	if strings.Contains(value, "/") {
		return strings.ReplaceAll(value, "/", ":")
	}

	ratio, err := strconv.ParseFloat(strings.ReplaceAll(value, ",", "."), 64)
	if err != nil || ratio <= 0 {
		return value
	}

	for _, candidate := range []struct{ num, den int }{{4, 3}, {16, 9}, {185, 100}, {239, 100}, {235, 100}} {
		target := float64(candidate.num) / float64(candidate.den)
		if math.Abs(ratio-target) <= 0.01 {
			return fmt.Sprintf("%d:%d", candidate.num, candidate.den)
		}
	}
	return value
}

// ResolveTracks 将 MediaInfo 的 DVD 字幕轨尽量映射回 ffprobe 的原始字幕 PID。
func ResolveTracks(raw []screenshotruntime.SubtitleTrack, tracks []screenshotruntime.DVDMediaInfoTrack) map[int]screenshotruntime.DVDMediaInfoTrack {
	resolved := make(map[int]screenshotruntime.DVDMediaInfoTrack, len(tracks))
	if len(raw) == 0 || len(tracks) == 0 {
		return resolved
	}

	rawPIDSet := make(map[int]struct{}, len(raw))
	for _, track := range raw {
		pid, ok := normalizeStreamPID(track.StreamID)
		if !ok {
			continue
		}
		rawPIDSet[pid] = struct{}{}
	}

	exactMatched := false
	for _, item := range tracks {
		if item.StreamID <= 0 {
			continue
		}
		if _, ok := rawPIDSet[item.StreamID]; ok {
			resolved[item.StreamID] = item
			exactMatched = true
		}
	}
	if exactMatched {
		return resolved
	}

	type rawTrackPID struct{ pid int }
	rawPIDs := make([]rawTrackPID, 0, len(raw))
	for _, track := range raw {
		if strings.ToLower(strings.TrimSpace(track.Codec)) != "dvd_subtitle" {
			continue
		}
		pid, ok := normalizeStreamPID(track.StreamID)
		if !ok {
			continue
		}
		rawPIDs = append(rawPIDs, rawTrackPID{pid: pid})
	}
	sort.Slice(rawPIDs, func(i, j int) bool { return rawPIDs[i].pid < rawPIDs[j].pid })

	mediaInfoCopy := append([]screenshotruntime.DVDMediaInfoTrack(nil), tracks...)
	sort.Slice(mediaInfoCopy, func(i, j int) bool {
		if mediaInfoCopy[i].StreamID != mediaInfoCopy[j].StreamID {
			return mediaInfoCopy[i].StreamID < mediaInfoCopy[j].StreamID
		}
		return mediaInfoCopy[i].ID < mediaInfoCopy[j].ID
	})

	limit := len(mediaInfoCopy)
	if len(rawPIDs) < limit {
		limit = len(rawPIDs)
	}
	for index := 0; index < limit; index++ {
		resolved[rawPIDs[index].pid] = mediaInfoCopy[index]
	}
	return resolved
}

func jsonString(value interface{}) string {
	switch typed := value.(type) {
	case string:
		return typed
	case fmt.Stringer:
		return typed.String()
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(typed), 'f', -1, 64)
	case int:
		return strconv.Itoa(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	default:
		return fmt.Sprint(typed)
	}
}

func normalizeStreamPID(raw string) (int, bool) {
	value := strings.ToLower(strings.TrimSpace(raw))
	value = strings.TrimPrefix(value, "0x")
	if strings.HasPrefix(strings.TrimSpace(raw), "0x") || strings.HasPrefix(strings.TrimSpace(raw), "0X") {
		parsed, err := strconv.ParseInt(value, 16, 64)
		if err != nil {
			return 0, false
		}
		return int(parsed), true
	}
	if parsed, err := strconv.Atoi(strings.TrimSpace(raw)); err == nil {
		return parsed, true
	}
	return 0, false
}
