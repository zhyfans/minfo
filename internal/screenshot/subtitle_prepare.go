// Package screenshot 负责字幕预载、阶段心跳和内封文字字幕提取准备。

package screenshot

import (
	"fmt"
	"os"
	"strings"
	"time"

	"minfo/internal/system"
)

// preloadDVDMediaInfo 会在 DVD 场景下提前读取 mediainfo，并输出对应阶段进度避免前端长时间停在等待状态。
func (r *screenshotRunner) preloadDVDMediaInfo() {
	if r == nil || !looksLikeDVDSource(r.dvdProbeSource()) || r.subtitleState.HasDVDMediaInfoResult || strings.TrimSpace(r.tools.MediaInfoBin) == "" {
		return
	}

	stage := "准备"
	current := 1
	total := 3
	detail := "正在读取 DVD MediaInfo 元数据。"
	if r.subtitleMode != SubtitleModeOff {
		stage = "字幕"
		detail = "正在读取 DVD MediaInfo 字幕元数据。"
	}

	r.logProgress(stage, current, total, detail)
	stopHeartbeat := r.startProgressHeartbeat(stage, detail)
	defer stopHeartbeat()

	_, _, _ = r.ensureDVDMediaInfoResult()
}

// startProgressHeartbeat 会周期性输出阶段心跳进度，并返回停止函数。
func (r *screenshotRunner) startProgressHeartbeat(stage, detail string) func() {
	if r == nil || strings.TrimSpace(stage) == "" || strings.TrimSpace(detail) == "" {
		return func() {}
	}

	startedAt := time.Now()
	done := make(chan struct{})
	var ctxDone <-chan struct{}
	if r.ctx != nil {
		ctxDone = r.ctx.Done()
	}

	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctxDone:
				return
			case <-done:
				return
			case <-ticker.C:
				elapsed := time.Since(startedAt)
				r.logProgressPercent(stage, subtitleHeartbeatStepPercent(elapsed), subtitleHeartbeatDetail(detail, elapsed))
			}
		}
	}()

	return func() {
		select {
		case <-done:
			return
		default:
			close(done)
		}
	}
}

// prepareTextSubtitleRenderSource 会为内封文字字幕准备更稳定的渲染来源。
func (r *screenshotRunner) prepareTextSubtitleRenderSource() error {
	if r.subtitle.Mode != "internal" {
		return nil
	}
	if r.isSupportedBitmapSubtitle() {
		return nil
	}
	if !isSupportedTextSubtitleCodec(r.subtitle.Codec) {
		return fmt.Errorf("unsupported text subtitle codec: %s", subtitleFormatLabel(r.subtitle.Codec))
	}
	if !r.shouldExtractInternalTextSubtitle() {
		return nil
	}

	pattern, extractionArgs, extractedCodec, logMessage := internalTextSubtitleExtractionPlan(r.subtitle.Codec)
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

	stdout, stderr, err := r.runFFmpegSubtitleExtract([]string{
		"-v", "error",
		"-i", r.sourcePath,
		"-map", fmt.Sprintf("0:s:%d", r.subtitle.RelativeIndex),
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

	r.subtitleState.TempSubtitleFile = tempPath
	r.subtitle.Mode = "external"
	r.subtitle.File = tempPath
	r.subtitle.Codec = extractedCodec
	r.subtitle.StreamIndex = -1
	r.subtitle.RelativeIndex = -1
	r.subtitle.ExtractedText = true
	r.logf("[信息] 已提取内封文本字幕供截图使用：%s", tempPath)
	return nil
}

// shouldExtractInternalTextSubtitle 会判断当前内封文字字幕是否需要先提取成临时文件。
func (r *screenshotRunner) shouldExtractInternalTextSubtitle() bool {
	if r == nil {
		return false
	}
	if r.subtitle.Mode != "internal" {
		return false
	}
	if r.isSupportedBitmapSubtitle() {
		return false
	}
	return true
}

// internalTextSubtitleExtractionPlan 会根据字幕 codec 返回提取策略和日志文案。
func internalTextSubtitleExtractionPlan(codec string) (pattern string, extractionCodecArg string, extractedCodec string, logMessage string) {
	switch strings.ToLower(strings.TrimSpace(codec)) {
	case "ass":
		return "minfo-sub-*.ass", "copy", "ass", "[信息] 内封 ASS 字幕将提取为原始 ASS 文件，保留样式后参与截图渲染。"
	case "ssa":
		return "minfo-sub-*.ssa", "copy", "ssa", "[信息] 内封 SSA 字幕将提取为原始 SSA 文件，保留样式后参与截图渲染。"
	default:
		return "minfo-sub-*.srt", "srt", "subrip", "[信息] 内封文字字幕将先提取为临时字幕文件，再参与截图渲染。"
	}
}
