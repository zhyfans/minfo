// Package screenshot 负责在文字字幕渲染前准备内封字体附件。

package screenshot

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"minfo/internal/system"
)

type subtitleFontAttachment struct {
	FileName string
	MimeType string
	Codec    string
}

// prepareEmbeddedSubtitleFonts 会在 ASS/SSA 场景下优先提取 MKV 附件字体供 libass 使用。
func (r *screenshotRunner) prepareEmbeddedSubtitleFonts() {
	if !r.shouldUseEmbeddedSubtitleFonts() {
		return
	}

	attachments, err := r.probeEmbeddedFontAttachments()
	if err != nil {
		r.logf("[提示] MKV 内封字体探测失败，将回退系统字体：%s", err.Error())
		return
	}
	if len(attachments) == 0 {
		return
	}

	fontDir, err := os.MkdirTemp("", "minfo-sub-fonts-*")
	if err != nil {
		r.logf("[提示] MKV 内封字体目录创建失败，将回退系统字体：%s", err.Error())
		return
	}

	stdout, stderr, err := system.RunCommandInDir(r.ctx, fontDir, r.ffmpegBin, buildEmbeddedFontExtractionArgs(r.sourcePath)...)
	if err != nil {
		_ = os.RemoveAll(fontDir)
		r.logf("[提示] MKV 内封字体提取失败，将回退系统字体：%s", system.BestErrorMessage(err, stderr, stdout))
		return
	}

	if countRegularFiles(fontDir) == 0 {
		_ = os.RemoveAll(fontDir)
		r.logf("[提示] 已识别到 MKV 字体附件，但未提取出可用字体文件，将回退系统字体。")
		return
	}

	r.subtitleFontDir = fontDir
	r.logf("[信息] 检测到 MKV 内封字体 %d 个，截图渲染将优先使用附件字体：%s",
		len(attachments),
		summarizeSubtitleFontAttachments(attachments),
	)
}

// shouldUseEmbeddedSubtitleFonts 会判断当前字幕场景是否值得优先提取 MKV 附件字体。
func (r *screenshotRunner) shouldUseEmbeddedSubtitleFonts() bool {
	if r == nil || r.subtitle.Mode == "none" {
		return false
	}

	switch strings.ToLower(strings.TrimSpace(r.subtitle.Codec)) {
	case "ass", "ssa":
	default:
		return false
	}

	switch strings.ToLower(strings.TrimSpace(filepath.Ext(r.sourcePath))) {
	case ".mkv", ".mk3d", ".mka", ".mks":
		return true
	default:
		return false
	}
}

// probeEmbeddedFontAttachments 会探测 Matroska 附件里是否存在字体文件。
func (r *screenshotRunner) probeEmbeddedFontAttachments() ([]subtitleFontAttachment, error) {
	args := []string{
		"-probesize", r.settings.ProbeSize,
		"-analyzeduration", r.settings.Analyze,
		"-v", "error",
		"-select_streams", "t",
		"-show_entries", "stream=codec_name:stream_tags=filename,mimetype",
		"-of", "json",
		r.sourcePath,
	}

	stdout, stderr, err := system.RunCommand(r.ctx, r.ffprobeBin, args...)
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

	attachments := make([]subtitleFontAttachment, 0, len(payload.Streams))
	for _, stream := range payload.Streams {
		fileName := attachmentTagValue(stream.Tags, "filename")
		mimeType := strings.ToLower(strings.TrimSpace(attachmentTagValue(stream.Tags, "mimetype")))
		codec := strings.ToLower(strings.TrimSpace(stream.CodecName))
		if !isFontAttachment(fileName, mimeType, codec) {
			continue
		}

		attachments = append(attachments, subtitleFontAttachment{
			FileName: fileName,
			MimeType: mimeType,
			Codec:    codec,
		})
	}
	return attachments, nil
}

// buildEmbeddedFontExtractionArgs 会构造抽取 Matroska 字体附件所需的 ffmpeg 参数。
func buildEmbeddedFontExtractionArgs(sourcePath string) []string {
	return []string{
		"-dump_attachment:t", "",
		"-v", "error",
		"-i", sourcePath,
		"-frames:v", "1",
		"-y",
		"-f", "null",
		"-",
	}
}

// attachmentTagValue 会读取附件标签中的指定字段。
func attachmentTagValue(tags map[string]interface{}, key string) string {
	if len(tags) == 0 {
		return ""
	}
	return strings.TrimSpace(jsonString(tags[key]))
}

// isFontAttachment 会根据文件名、mime 和 codec 粗略判断附件是否为字体。
func isFontAttachment(fileName, mimeType, codec string) bool {
	lowerName := strings.ToLower(strings.TrimSpace(fileName))
	switch filepath.Ext(lowerName) {
	case ".ttf", ".ttc", ".otf", ".otc", ".woff", ".woff2":
		return true
	}

	switch strings.TrimSpace(mimeType) {
	case "font/ttf",
		"font/otf",
		"font/collection",
		"font/woff",
		"font/woff2",
		"application/x-truetype-font",
		"application/x-font-ttf",
		"application/x-font-otf",
		"application/vnd.ms-opentype",
		"application/font-sfnt":
		return true
	}

	switch strings.TrimSpace(codec) {
	case "ttf", "otf", "woff", "woff2":
		return true
	default:
		return false
	}
}

// summarizeSubtitleFontAttachments 会把附件字体名压缩成简短稳定的日志文案。
func summarizeSubtitleFontAttachments(attachments []subtitleFontAttachment) string {
	if len(attachments) == 0 {
		return "无"
	}

	names := make([]string, 0, len(attachments))
	for _, item := range attachments {
		name := strings.TrimSpace(item.FileName)
		if name == "" {
			name = subtitleFontAttachmentLabel(item)
		}
		names = append(names, name)
	}

	sort.Strings(names)
	if len(names) > 5 {
		return strings.Join(names[:5], ", ") + fmt.Sprintf(" 等 %d 个", len(names))
	}
	return strings.Join(names, ", ")
}

// subtitleFontAttachmentLabel 会为没有文件名的附件生成可读标识。
func subtitleFontAttachmentLabel(item subtitleFontAttachment) string {
	if strings.TrimSpace(item.MimeType) != "" {
		return item.MimeType
	}
	if strings.TrimSpace(item.Codec) != "" {
		return item.Codec
	}
	return "unknown-font"
}

// countRegularFiles 会统计目录下提取出的普通文件数量。
func countRegularFiles(dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}

	total := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		total++
	}
	return total
}
