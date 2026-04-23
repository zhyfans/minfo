package subtitle

import (
	"fmt"
	"os"
	"strings"

	"minfo/internal/screenshot/source"
	"minfo/internal/system"
)

// PreloadDVDMediaInfo 会在 DVD 场景下提前读取 mediainfo，并输出对应阶段进度避免前端长时间停在等待状态。
func (r *Runner) PreloadDVDMediaInfo() {
	if r == nil || !source.LooksLikeDVDSource(r.dvdProbeSource()) || r.state().HasDVDMediaInfoResult || strings.TrimSpace(r.Tools.MediaInfoBin) == "" {
		return
	}

	stage := "准备"
	current := 1
	total := 3
	detail := "正在读取 DVD MediaInfo 元数据。"
	if r.SubtitleMode != subtitleModeOff {
		stage = "字幕"
		detail = "正在读取 DVD MediaInfo 字幕元数据。"
	}

	r.logProgress(stage, current, total, detail)
	stopHeartbeat := r.startHeartbeat(stage, detail)
	defer stopHeartbeat()

	_, _, _ = r.ensureDVDMediaInfoResult()
}

// PrepareTextSubtitleRenderSource 会为内封文字字幕准备更稳定的渲染来源。
func (r *Runner) PrepareTextSubtitleRenderSource() error {
	selection := r.selection()
	if selection.Mode != "internal" {
		return nil
	}
	if r.isSupportedBitmapSubtitle() {
		return nil
	}
	if !IsSupportedTextCodec(selection.Codec) {
		return fmt.Errorf("unsupported text subtitle codec: %s", FormatLabel(selection.Codec))
	}
	if !r.ShouldExtractInternalTextSubtitle() {
		return nil
	}

	pattern, extractionArgs, extractedCodec, logMessage := InternalTextSubtitleExtractionPlan(selection.Codec)
	r.logProgress("字幕", 3, 3, "正在提取内封文字字幕。")
	r.logf("%s", logMessage)

	tempFile, err := os.CreateTemp("", pattern)
	if err != nil {
		return err
	}
	tempPath := tempFile.Name()
	if closeErr := tempFile.Close(); closeErr != nil {
		_ = os.Remove(tempPath)
		return closeErr
	}

	stdout, stderr, err := r.runSubtitleExtract([]string{
		"-v", "error",
		"-i", r.SourcePath,
		"-map", fmt.Sprintf("0:s:%d", selection.RelativeIndex),
		"-c:s", extractionArgs,
		"-y", tempPath,
	})
	if err != nil {
		_ = os.Remove(tempPath)
		if message := strings.TrimSpace(system.BestErrorMessage(err, stderr, stdout)); message != "" {
			normalized := strings.ReplaceAll(message, "\r\n", "\n")
			normalized = strings.ReplaceAll(normalized, "\r", "\n")
			for _, line := range strings.Split(normalized, "\n") {
				if strings.TrimSpace(line) == "" {
					continue
				}
				r.logf("[错误] 提取失败详情: %s", line)
			}
		}
		return fmt.Errorf("failed to extract internal text subtitle: %w", err)
	}

	state := r.state()
	state.TempSubtitleFile = tempPath
	selection.Mode = "external"
	selection.File = tempPath
	selection.Codec = extractedCodec
	selection.StreamIndex = -1
	selection.RelativeIndex = -1
	selection.ExtractedText = true
	r.logf("[信息] 已提取内封文本字幕供截图使用：%s", tempPath)
	return nil
}

// ShouldExtractInternalTextSubtitle 会判断当前内封文字字幕是否需要先提取成临时文件。
func (r *Runner) ShouldExtractInternalTextSubtitle() bool {
	selection := r.selection()
	if selection.Mode != "internal" {
		return false
	}
	if r.isSupportedBitmapSubtitle() {
		return false
	}
	return true
}

// InternalTextSubtitleExtractionPlan 会根据字幕 codec 返回提取策略和日志文案。
func InternalTextSubtitleExtractionPlan(codec string) (pattern string, extractionCodecArg string, extractedCodec string, logMessage string) {
	switch strings.ToLower(strings.TrimSpace(codec)) {
	case "ass":
		return "minfo-sub-*.ass", "copy", "ass", "[信息] 内封 ASS 字幕将提取为原始 ASS 文件，保留样式后参与截图渲染。"
	case "ssa":
		return "minfo-sub-*.ssa", "copy", "ssa", "[信息] 内封 SSA 字幕将提取为原始 SSA 文件，保留样式后参与截图渲染。"
	default:
		return "minfo-sub-*.srt", "srt", "subrip", "[信息] 内封文字字幕将先提取为临时字幕文件，再参与截图渲染。"
	}
}
