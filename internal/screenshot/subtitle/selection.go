// Package subtitle 实现字幕选择入口与最终摘要日志。

package subtitle

import (
	"strings"

	screenshotruntime "minfo/internal/screenshot/runtime"
)

// Choose 会在当前截图上下文中确定最终使用的字幕来源，并把结果写回运行器状态。
func (r *Runner) Choose() error {
	selection := r.selection()
	*selection = screenshotruntime.SubtitleSelection{Mode: "none", RelativeIndex: -1, StreamIndex: -1}

	if r.SubtitleMode == subtitleModeOff {
		r.logf("[信息] 已禁用字幕挂载与字幕对齐，将直接按时间点截图。")
		return nil
	}

	if picked, ok, err := r.findExternalSubtitle(); err != nil {
		return err
	} else if ok {
		*selection = picked
		r.logSubtitleFallback("外挂")
		return nil
	}

	if picked, ok, err := r.pickInternalSubtitle(); err != nil {
		return err
	} else if ok {
		*selection = picked
		r.logSubtitleFallback("内封")
		return nil
	}

	r.logf("[提示] 未找到可用字幕，将仅截图视频画面。")
	return nil
}

// LogSelectedSubtitleSummary 记录最终选中的字幕来源、格式和渲染方式。
func (r *Runner) LogSelectedSubtitleSummary() {
	selection := r.selection()
	if selection.Mode == "none" {
		return
	}

	source := "外挂"
	render := "直接使用外挂文件"
	if selection.ExtractedText {
		source = "内封"
		render = "提取内封文字字幕后按外挂文件渲染"
	} else if selection.Mode == "internal" {
		source = "内封"
		render = "直接使用内封轨道"
	}
	if strings.TrimSpace(r.state().SubtitleFontDir) != "" {
		render += "（优先使用 MKV 附件字体）"
	}

	r.logf("[字幕格式] 来源：%s | 格式：%s | 渲染：%s", source, FormatLabel(selection.Codec), render)
}

func (r *Runner) logSubtitleFallback(modeLabel string) {
	switch r.selection().Lang {
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
