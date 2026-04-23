// Package screenshot 提供字幕格式、语言和标签解析相关辅助函数。

package screenshot

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// subtitleCodecFromPath 根据字幕文件扩展名推断 ffmpeg 使用的 codec 名称。
func subtitleCodecFromPath(path string) string {
	switch strings.ToLower(strings.TrimSpace(filepath.Ext(path))) {
	case ".ass":
		return "ass"
	case ".ssa":
		return "ssa"
	case ".srt":
		return "subrip"
	default:
		return strings.TrimPrefix(strings.ToLower(strings.TrimSpace(filepath.Ext(path))), ".")
	}
}

// subtitleFormatLabel 把字幕 codec 转换成更适合日志展示的格式名称。
func subtitleFormatLabel(codec string) string {
	switch strings.ToLower(strings.TrimSpace(codec)) {
	case "subrip", "srt":
		return "SRT/SubRip"
	case "ass":
		return "ASS"
	case "ssa":
		return "SSA"
	case "hdmv_pgs_subtitle", "pgssub":
		return "PGS"
	case "dvd_subtitle":
		return "DVD Subtitle"
	case "dvb_subtitle":
		return "DVB Subtitle"
	case "xsub":
		return "XSub"
	case "vobsub":
		return "VobSub"
	case "":
		return "未知"
	default:
		return strings.ToUpper(codec)
	}
}

// subtitleHandlingLabel 返回当前字幕 codec 在截图流程里的处理方式说明。
func subtitleHandlingLabel(codec string) string {
	switch {
	case bitmapSubtitleKindFromCodec(codec) == bitmapSubtitlePGS:
		return "PGS位图"
	case bitmapSubtitleKindFromCodec(codec) == bitmapSubtitleDVD:
		return "DVD位图"
	case isUnsupportedBitmapSubtitleCodec(codec):
		return "暂不支持位图"
	default:
		return "文字字幕"
	}
}

// isSupportedTextSubtitleCodec 会判断当前文字字幕 codec 是否在服务端允许范围内。
func isSupportedTextSubtitleCodec(codec string) bool {
	switch strings.ToLower(strings.TrimSpace(codec)) {
	case "ass", "ssa", "subrip", "srt":
		return true
	default:
		return false
	}
}

// isKnownTextSubtitleExtension 会判断文件扩展名是否属于当前识别范围内的文本字幕格式。
func isKnownTextSubtitleExtension(ext string) bool {
	switch strings.ToLower(strings.TrimSpace(ext)) {
	case ".ass", ".ssa", ".srt", ".vtt", ".webvtt", ".ttml", ".dfxp", ".smi", ".sami", ".stl", ".sbv", ".lrc":
		return true
	default:
		return false
	}
}

// isSupportedTextSubtitlePath 会判断外挂文本字幕文件是否在当前允许范围内。
func isSupportedTextSubtitlePath(path string) bool {
	if !isKnownTextSubtitleExtension(filepath.Ext(path)) {
		return false
	}
	return isSupportedTextSubtitleCodec(subtitleCodecFromPath(path))
}

// tracksHaveClassifiedLang 判断原始字幕轨中是否已有可识别的目标语言标签。
func tracksHaveClassifiedLang(tracks []subtitleTrack) bool {
	for _, track := range tracks {
		if classifySubtitleLanguage(strings.TrimSpace(track.Language+" "+track.Title)) != "" {
			return true
		}
	}
	return false
}

// helperTracksHaveClassifiedLang 判断 bdsub 返回的轨道里是否已有可识别的目标语言标签。
func helperTracksHaveClassifiedLang(tracks []blurayHelperTrack) bool {
	for _, track := range tracks {
		if classifySubtitleLanguage(track.Lang) != "" {
			return true
		}
	}
	return false
}

// subtitleNeedsBluraySupplement 判断当前语言信息是否仍然完全不足以参与蓝光字幕选轨。
func subtitleNeedsBluraySupplement(lang, title string) bool {
	class := classifySubtitleLanguage(strings.TrimSpace(lang + " " + title))
	return class == ""
}

// classifySubtitleLanguage 把语言和标题文本归类为 zh-Hans、zh-Hant、zh、en 或空值。
func classifySubtitleLanguage(input string) string {
	value := strings.ToLower(strings.TrimSpace(input))
	if value == "" {
		return ""
	}

	if containsAnyToken(value, langZHHansTokens) {
		return "zh-Hans"
	}
	if containsAnyToken(value, langZHHantTokens) {
		return "zh-Hant"
	}
	if containsAnyToken(value, langZHTokens) {
		return "zh"
	}
	if containsAnyToken(value, langENTokens) {
		return "en"
	}
	return ""
}

// containsAnyToken 会判断任意令牌是否包含目标内容。
func containsAnyToken(haystack string, tokens []string) bool {
	for _, token := range tokens {
		if strings.Contains(haystack, strings.ToLower(token)) {
			return true
		}
	}
	return false
}

// subtitleLanguageScore 返回字幕语言优先级分数。
func subtitleLanguageScore(lang string) int {
	switch lang {
	case "zh-Hans":
		return 400
	case "zh-Hant":
		return 300
	case "zh":
		return 250
	case "en":
		return 200
	default:
		return 0
	}
}

// subtitleDispositionScore 根据 forced 和 default 标记计算附加优先级。
func subtitleDispositionScore(forced, isDefault int) int {
	switch {
	case forced == 0 && isDefault == 1:
		return 40
	case forced == 0 && isDefault == 0:
		return 30
	case forced == 1 && isDefault == 1:
		return 20
	default:
		return 10
	}
}

// preferPreferredSubtitleRank 比较两个字幕候选排名，并判断 current 是否更优。
func preferPreferredSubtitleRank(current, best preferredSubtitleRank) bool {
	if current.LangScore != best.LangScore {
		return current.LangScore > best.LangScore
	}
	if current.LangClass != "" &&
		current.LangClass == best.LangClass &&
		current.BitmapKind == bitmapSubtitlePGS &&
		best.BitmapKind == bitmapSubtitlePGS &&
		(current.UsePayloadBytes || best.UsePayloadBytes) &&
		current.PayloadBytes != best.PayloadBytes {
		return current.PayloadBytes > best.PayloadBytes
	}
	if current.LangClass != "" &&
		current.LangClass == best.LangClass &&
		current.BitmapKind == bitmapSubtitlePGS &&
		best.BitmapKind == bitmapSubtitlePGS &&
		(current.UseBitrate || best.UseBitrate) &&
		current.Bitrate != best.Bitrate {
		return current.Bitrate > best.Bitrate
	}
	if current.DispositionScore != best.DispositionScore {
		return current.DispositionScore > best.DispositionScore
	}
	switch {
	case current.PIDOK && best.PIDOK:
		return current.PID < best.PID
	case current.PIDOK:
		return true
	default:
		return false
	}
}

// firstSubtitleLanguage 从标签集合中读取最可能的语言字段。
func firstSubtitleLanguage(tags map[string]interface{}) string {
	for _, key := range []string{"language", "lang"} {
		if value := lookupTag(tags, key); value != "" {
			return value
		}
	}
	for _, prefix := range []string{"language-", "language_", "lang-", "lang_"} {
		if value := lookupTagPrefix(tags, prefix); value != "" {
			return value
		}
	}
	return ""
}

// firstSubtitleTitle 从标签集合中读取最可能的标题字段。
func firstSubtitleTitle(tags map[string]interface{}) string {
	for _, key := range []string{"title", "name", "handler_name"} {
		if value := lookupTag(tags, key); value != "" {
			return value
		}
	}
	for _, prefix := range []string{"title-", "title_", "name-", "name_", "handler_name-", "handler_name_"} {
		if value := lookupTagPrefix(tags, prefix); value != "" {
			return value
		}
	}
	return ""
}

// lookupTag 按大小写不敏感的精确键名读取标签值。
func lookupTag(tags map[string]interface{}, wanted string) string {
	for key, value := range tags {
		if strings.EqualFold(strings.TrimSpace(key), wanted) {
			return strings.TrimSpace(jsonString(value))
		}
	}
	return ""
}

// lookupTagPrefix 按大小写不敏感的前缀读取标签值。
func lookupTagPrefix(tags map[string]interface{}, prefix string) string {
	for key, value := range tags {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(key)), prefix) {
			return strings.TrimSpace(jsonString(value))
		}
	}
	return ""
}

// subtitleTagsSummary 把字幕标签映射稳定地序列化为日志字符串。
func subtitleTagsSummary(tags map[string]interface{}) string {
	if len(tags) == 0 {
		return ""
	}

	keys := make([]string, 0, len(tags))
	for key := range tags {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		value := strings.TrimSpace(jsonString(tags[key]))
		if value == "" {
			continue
		}
		parts = append(parts, key+"="+value)
	}
	return strings.Join(parts, "; ")
}

// jsonString 把 JSON 解析后的任意值转换成字符串表示。
func jsonString(value interface{}) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case json.Number:
		return typed.String()
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case int:
		return strconv.Itoa(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	default:
		return fmt.Sprint(typed)
	}
}
