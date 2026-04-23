// Package screenshot 实现字幕选择入口编排。

package screenshot

// chooseSubtitle 会在当前截图上下文中确定最终使用的字幕来源，并把结果写回运行器状态。
func (r *screenshotRunner) chooseSubtitle() error {
	r.subtitle = subtitleSelection{Mode: "none", RelativeIndex: -1, StreamIndex: -1}

	if r.subtitleMode == SubtitleModeOff {
		r.logf("[信息] 已禁用字幕挂载与字幕对齐，将直接按时间点截图。")
		return nil
	}

	if selection, ok, err := r.findExternalSubtitle(); err != nil {
		return err
	} else if ok {
		r.subtitle = selection
		r.logSubtitleFallback("外挂")
		return nil
	}

	if selection, ok, err := r.pickInternalSubtitle(); err != nil {
		return err
	} else if ok {
		r.subtitle = selection
		r.logSubtitleFallback("内封")
		return nil
	}

	r.logf("[提示] 未找到可用字幕，将仅截图视频画面。")
	return nil
}
