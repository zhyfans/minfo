// Package subtitle 提供截图字幕流程可复用的纯辅助逻辑。

package subtitle

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	screenshotruntime "minfo/internal/screenshot/runtime"
)

// CodecFromPath 根据字幕文件扩展名推断 ffmpeg 使用的 codec 名称。
func CodecFromPath(path string) string {
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

// FormatLabel 把字幕 codec 转换成更适合日志展示的格式名称。
func FormatLabel(codec string) string {
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

// HandlingLabel 返回当前字幕 codec 在截图流程里的处理方式说明。
func HandlingLabel(codec string) string {
	switch {
	case BitmapKindFromCodec(codec) == screenshotruntime.BitmapSubtitlePGS:
		return "PGS位图"
	case BitmapKindFromCodec(codec) == screenshotruntime.BitmapSubtitleDVD:
		return "DVD位图"
	case IsUnsupportedBitmapCodec(codec):
		return "暂不支持位图"
	default:
		return "文字字幕"
	}
}

// IsSupportedTextCodec 会判断当前文字字幕 codec 是否在服务端允许范围内。
func IsSupportedTextCodec(codec string) bool {
	switch strings.ToLower(strings.TrimSpace(codec)) {
	case "ass", "ssa", "subrip", "srt":
		return true
	default:
		return false
	}
}

// IsKnownTextExtension 会判断文件扩展名是否属于当前识别范围内的文本字幕格式。
func IsKnownTextExtension(ext string) bool {
	switch strings.ToLower(strings.TrimSpace(ext)) {
	case ".ass", ".ssa", ".srt", ".vtt", ".webvtt", ".ttml", ".dfxp", ".smi", ".sami", ".stl", ".sbv", ".lrc":
		return true
	default:
		return false
	}
}

// IsSupportedTextPath 会判断外挂文本字幕文件是否在当前允许范围内。
func IsSupportedTextPath(path string) bool {
	if !IsKnownTextExtension(filepath.Ext(path)) {
		return false
	}
	return IsSupportedTextCodec(CodecFromPath(path))
}

// TracksHaveClassifiedLang 判断原始字幕轨中是否已有可识别的目标语言标签。
func TracksHaveClassifiedLang(tracks []screenshotruntime.SubtitleTrack) bool {
	for _, track := range tracks {
		if ClassifyLanguage(strings.TrimSpace(track.Language+" "+track.Title)) != "" {
			return true
		}
	}
	return false
}

// HelperTracksHaveClassifiedLang 判断 bdsub 返回的轨道里是否已有可识别的目标语言标签。
func HelperTracksHaveClassifiedLang(tracks []screenshotruntime.BlurayHelperTrack) bool {
	for _, track := range tracks {
		if ClassifyLanguage(track.Lang) != "" {
			return true
		}
	}
	return false
}

// NeedsBluraySupplement 判断当前语言信息是否仍然完全不足以参与蓝光字幕选轨。
func NeedsBluraySupplement(lang, title string) bool {
	class := ClassifyLanguage(strings.TrimSpace(lang + " " + title))
	return class == ""
}

// ClassifyLanguage 把语言和标题文本归类为 zh-Hans、zh-Hant、zh、en 或空值。
func ClassifyLanguage(input string) string {
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

func containsAnyToken(haystack string, tokens []string) bool {
	for _, token := range tokens {
		if strings.Contains(haystack, strings.ToLower(token)) {
			return true
		}
	}
	return false
}

// LanguageScore 返回字幕语言优先级分数。
func LanguageScore(lang string) int {
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

// DispositionScore 根据 forced 和 default 标记计算附加优先级。
func DispositionScore(forced, isDefault int) int {
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

// PreferRank 比较两个字幕候选排名，并判断 current 是否更优。
func PreferRank(current, best screenshotruntime.PreferredSubtitleRank) bool {
	if current.LangScore != best.LangScore {
		return current.LangScore > best.LangScore
	}
	if current.LangClass != "" &&
		current.LangClass == best.LangClass &&
		current.BitmapKind == screenshotruntime.BitmapSubtitlePGS &&
		best.BitmapKind == screenshotruntime.BitmapSubtitlePGS &&
		(current.UsePayloadBytes || best.UsePayloadBytes) &&
		current.PayloadBytes != best.PayloadBytes {
		return current.PayloadBytes > best.PayloadBytes
	}
	if current.LangClass != "" &&
		current.LangClass == best.LangClass &&
		current.BitmapKind == screenshotruntime.BitmapSubtitlePGS &&
		best.BitmapKind == screenshotruntime.BitmapSubtitlePGS &&
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

// FirstLanguage 从标签集合中读取最可能的语言字段。
func FirstLanguage(tags map[string]interface{}) string {
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

// FirstTitle 从标签集合中读取最可能的标题字段。
func FirstTitle(tags map[string]interface{}) string {
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

func lookupTag(tags map[string]interface{}, wanted string) string {
	for key, value := range tags {
		if strings.EqualFold(strings.TrimSpace(key), wanted) {
			return strings.TrimSpace(JSONString(value))
		}
	}
	return ""
}

func lookupTagPrefix(tags map[string]interface{}, prefix string) string {
	for key, value := range tags {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(key)), prefix) {
			return strings.TrimSpace(JSONString(value))
		}
	}
	return ""
}

// TagsSummary 把字幕标签映射稳定地序列化为日志字符串。
func TagsSummary(tags map[string]interface{}) string {
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
		value := strings.TrimSpace(JSONString(tags[key]))
		if value == "" {
			continue
		}
		parts = append(parts, key+"="+value)
	}
	return strings.Join(parts, "; ")
}

// JSONString 把 JSON 解析后的任意值转换成字符串表示。
func JSONString(value interface{}) string {
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

// NormalizeStreamPID 会规范化流 PID，并在输入为空或不受支持时返回稳定的默认值。
func NormalizeStreamPID(raw string) (int, bool) {
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

// FormatStreamPID 把流 PID 格式化为十六进制字符串。
func FormatStreamPID(value int) string {
	return fmt.Sprintf("0x%04X", value)
}

// BitmapKindFromCodec 把 codec 名称映射到内部使用的位图字幕类型枚举。
func BitmapKindFromCodec(codec string) screenshotruntime.BitmapSubtitleKind {
	switch strings.ToLower(strings.TrimSpace(codec)) {
	case "hdmv_pgs_subtitle", "pgssub":
		return screenshotruntime.BitmapSubtitlePGS
	case "dvd_subtitle":
		return screenshotruntime.BitmapSubtitleDVD
	default:
		return screenshotruntime.BitmapSubtitleNone
	}
}

// IsUnsupportedBitmapCodec 会判断当前 codec 是否属于暂不支持的位图字幕类型。
func IsUnsupportedBitmapCodec(codec string) bool {
	switch strings.ToLower(strings.TrimSpace(codec)) {
	case "dvb_subtitle", "xsub", "vobsub":
		return true
	default:
		return false
	}
}

var (
	langZHHansTokens = []string{"zh-hans", "zh_cn", "zh-cn", "chs", "sc", "简体", "简中", "gb", "gb2312", "国配简中", "简日", "简英"}
	langZHHantTokens = []string{"zh-hant", "zh_tw", "zh-tw", "cht", "tc", "繁体", "繁中", "big5", "繁日", "繁英"}
	langZHTokens     = []string{"chi", "zho", "chinese", "中文", "双语", "中字"}
	langENTokens     = []string{"eng", "english", "英文", "英字", "en"}
)
