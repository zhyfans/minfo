// Package screenshot 负责截图运行器的执行编排与结果汇总。

package screenshot

import (
	"errors"
	"fmt"
	"path/filepath"
)

// screenshotRunState 维护单轮截图执行过程中需要累计的中间状态。
type screenshotRunState struct {
	totalShots     int
	startedShots   int
	processedShots int
	successCount   int
	failures       []string
	usedNames      map[string]int
	usedSeconds    map[int]struct{}
}

// screenshotCapturePlan 描述一张截图在真正渲染前已经确定的输出计划。
type screenshotCapturePlan struct {
	aligned    float64
	outputName string
	outputPath string
}

// newScreenshotRunState 会为当前批次截图请求创建新的执行状态容器。
func newScreenshotRunState(totalShots int) *screenshotRunState {
	return &screenshotRunState{
		totalShots:  totalShots,
		failures:    make([]string, 0),
		usedNames:   make(map[string]int, totalShots),
		usedSeconds: make(map[int]struct{}, totalShots),
	}
}

// nextShotIndex 会返回下一张待开始截图的展示序号。
func (s *screenshotRunState) nextShotIndex() int {
	return s.startedShots + 1
}

// beginShot 会标记一张截图正式进入渲染阶段，并返回其展示序号。
func (s *screenshotRunState) beginShot() int {
	s.startedShots++
	return s.startedShots
}

// markFailed 会记录一张截图失败，并返回失败后累计完成数。
func (s *screenshotRunState) markFailed(outputName string, err error) int {
	s.failures = append(s.failures, fmt.Sprintf("[失败] 文件: %s\n原因: %s", outputName, err.Error()))
	s.processedShots++
	return s.processedShots
}

// markSucceeded 会记录一张截图成功，并返回成功后累计完成数。
func (s *screenshotRunState) markSucceeded(aligned float64) int {
	s.usedSeconds[screenshotSecond(aligned)] = struct{}{}
	s.successCount++
	s.processedShots++
	return s.processedShots
}

// run 会按请求时间点执行整轮截图流程，并汇总成功、失败和最终输出文件。
func (r *screenshotRunner) run() ([]string, error) {
	state := newScreenshotRunState(len(r.requested))
	for _, requested := range r.requested {
		r.runRequestedScreenshot(requested, state)
	}
	return r.finalizeScreenshotRun(state)
}

// runRequestedScreenshot 会完成单个请求时间点从对齐到渲染的整套流程。
func (r *screenshotRunner) runRequestedScreenshot(requested float64, state *screenshotRunState) {
	plan, ok := r.prepareScreenshotCapture(requested, state)
	if !ok {
		return
	}
	r.capturePreparedScreenshot(plan, state)
}

// prepareScreenshotCapture 会为一张截图完成时间点对齐、去重和输出路径规划。
func (r *screenshotRunner) prepareScreenshotCapture(requested float64, state *screenshotRunState) (screenshotCapturePlan, bool) {
	r.activeShot.Prepare(state.nextShotIndex(), state.totalShots)

	aligned, ok := r.resolveAlignedScreenshotTime(requested, state)
	if !ok {
		r.activeShot.Reset()
		return screenshotCapturePlan{}, false
	}

	outputName := uniqueScreenshotName(aligned, r.settings.Ext, state.usedNames)
	outputPath := filepath.Join(r.outputDir, outputName)
	r.logf("[信息] 截图: 请求 %s → 对齐 %s → 输出 %s -> %s",
		secToHMSMS(requested),
		secToHMSMS(aligned),
		secToHMSMS(aligned),
		outputName,
	)

	current := state.beginShot()
	r.activeShot.BeginRender(current, state.totalShots, outputName)
	r.logProgress("截图开始", current, state.totalShots, fmt.Sprintf("正在渲染第 %d/%d 张截图：%s", current, state.totalShots, outputName))

	return screenshotCapturePlan{
		aligned:    aligned,
		outputName: outputName,
		outputPath: outputPath,
	}, true
}

// resolveAlignedScreenshotTime 会完成字幕对齐、时长裁剪和唯一秒去重。
func (r *screenshotRunner) resolveAlignedScreenshotTime(requested float64, state *screenshotRunState) (float64, bool) {
	aligned := requested
	if r.subtitle.Mode != "none" {
		r.logShotAlignmentProgress()
		aligned = r.alignToSubtitle(requested)
	}
	aligned = r.clampToDuration(aligned)

	candidate, adjusted, ok := r.resolveUniqueScreenshotSecond(requested, aligned, state.usedSeconds)
	if !ok {
		r.logf("[提示] 请求 %s 对齐后未找到新的唯一秒，跳过该截图。", secToHMSMS(requested))
		return 0, false
	}
	if adjusted {
		r.logf("[提示] 请求 %s 对齐后命中已使用秒，改用唯一秒 %s",
			secToHMSMS(requested),
			secToHMSMS(candidate),
		)
	}
	return candidate, true
}

// capturePreparedScreenshot 会执行一张已完成规划的截图，并回写执行结果。
func (r *screenshotRunner) capturePreparedScreenshot(plan screenshotCapturePlan, state *screenshotRunState) {
	defer r.activeShot.Reset()

	if err := r.captureScreenshot(plan.aligned, plan.outputPath); err != nil {
		processed := state.markFailed(plan.outputName, err)
		r.logProgress("截图完成", processed, state.totalShots, fmt.Sprintf("第 %d/%d 张截图失败：%s", processed, state.totalShots, plan.outputName))
		return
	}

	processed := state.markSucceeded(plan.aligned)
	r.logProgress("截图完成", processed, state.totalShots, fmt.Sprintf("已完成第 %d/%d 张截图：%s", processed, state.totalShots, plan.outputName))
}

// finalizeScreenshotRun 会输出整轮截图摘要，并返回最终文件列表。
func (r *screenshotRunner) finalizeScreenshotRun(state *screenshotRunState) ([]string, error) {
	r.logScreenshotRunSummary(state)

	r.logProgress("整理", 1, 4, "正在整理截图文件列表。")
	files, err := listScreenshotFiles(r.outputDir)
	if err != nil {
		if state.successCount == 0 {
			return nil, errors.New("no screenshots were generated")
		}
		return nil, err
	}
	return files, nil
}

// logScreenshotRunSummary 会输出本轮截图任务的成功/失败摘要和失败详情。
func (r *screenshotRunner) logScreenshotRunSummary(state *screenshotRunState) {
	r.logf("")
	r.logf("===== 任务完成 =====")
	r.logf("成功: %d 张 | 失败: %d 张", state.successCount, len(state.failures))

	if len(state.failures) == 0 {
		return
	}

	r.logf("")
	r.logf("===== 失败详情 =====")
	for _, item := range state.failures {
		r.logf("%s", item)
	}
}
