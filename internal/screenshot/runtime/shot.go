package runtime

import (
	"fmt"
	"strings"
)

const (
	// ActiveShotPhaseRender 表示当前截图处于主渲染阶段。
	ActiveShotPhaseRender = "render"
	// ActiveShotPhaseReencode 表示当前截图处于重编码/重拍阶段。
	ActiveShotPhaseReencode = "reencode"
)

// ActiveShot 维护当前正在处理的截图上下文。
type ActiveShot struct {
	index int
	total int
	name  string
	phase string
}

// Prepare 会记录当前即将处理的截图序号，并清空渲染阶段上下文。
func (s *ActiveShot) Prepare(index, total int) {
	if s == nil {
		return
	}
	s.index = index
	s.total = total
	s.name = ""
	s.phase = ""
}

// BeginRender 会记录当前截图已进入渲染阶段。
func (s *ActiveShot) BeginRender(index, total int, name string) {
	if s == nil {
		return
	}
	s.index = index
	s.total = total
	s.name = strings.TrimSpace(name)
	s.phase = ActiveShotPhaseRender
}

// SetPhase 会更新当前截图所处的渲染子阶段。
func (s *ActiveShot) SetPhase(phase string) {
	if s == nil {
		return
	}
	s.phase = strings.TrimSpace(phase)
}

// Reset 会清空当前截图上下文。
func (s *ActiveShot) Reset() {
	if s == nil {
		return
	}
	s.index = 0
	s.total = 0
	s.name = ""
	s.phase = ""
}

// Current 返回当前截图序号。
func (s ActiveShot) Current() int {
	return s.index
}

// Total 返回当前批次总截图数。
func (s ActiveShot) Total() int {
	return s.total
}

// Phase 返回当前截图渲染子阶段。
func (s ActiveShot) Phase() string {
	return s.phase
}

// Active 返回当前是否已经绑定有效的截图序号。
func (s ActiveShot) Active() bool {
	return s.index > 0 && s.total > 0
}

// ProgressLabel 返回当前截图渲染阶段适合展示的说明文本。
func (s ActiveShot) ProgressLabel() string {
	if !s.Active() || s.name == "" {
		return "正在渲染截图。"
	}
	if s.phase == ActiveShotPhaseReencode {
		return fmt.Sprintf("正在重拍第 %d/%d 张截图：%s", s.index, s.total, s.name)
	}
	return fmt.Sprintf("正在渲染第 %d/%d 张截图：%s", s.index, s.total, s.name)
}

// AlignmentDetail 返回当前截图进入字幕对齐阶段时的进度文案。
func (s ActiveShot) AlignmentDetail() string {
	if !s.Active() {
		return ""
	}
	return fmt.Sprintf("正在对齐第 %d/%d 张截图时间点...", s.index, s.total)
}

// BitmapVisibilityDetail 返回当前截图进入位图字幕可见性校验阶段时的进度文案。
func (s ActiveShot) BitmapVisibilityDetail(label string) string {
	if !s.Active() {
		return ""
	}
	label = strings.TrimSpace(label)
	if label == "" {
		label = "PGS/DVD"
	}
	return fmt.Sprintf("正在校验 %s 字幕是否可见...", label)
}
