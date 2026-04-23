// Package handlers 提供后台任务的阶段型进度推导。

package handlers

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"minfo/internal/httpapi/transport"
	"minfo/internal/screenshot"
)

var (
	mediainfoCandidatesPattern   = regexp.MustCompile(`^\[mediainfo\] 候选源数量: (\d+)$`)
	mediainfoAttemptPattern      = regexp.MustCompile(`^\[mediainfo\] 尝试 (\d+)/(\d+): `)
	bdinfoScanProgressPattern    = regexp.MustCompile(`^\[bdinfo\]\[(?:stdout|stderr)\] Scanning\s+(\d+)%\s+-\s+(.+)$`)
	screenshotProgressPattern    = regexp.MustCompile(`^\[进度\] ([^ ]+) (\d+)/(\d+): (.+)$`)
	screenshotPercentPattern     = regexp.MustCompile(`^\[进度\] ([^ ]+) (\d+(?:\.\d+)?)%: (.+)$`)
	screenshotUploadStartPattern = regexp.MustCompile(`^开始处理 (\d+) 个文件\.\.\.$`)
)

type screenshotProgressMarker struct {
	current      int
	total        int
	percent      float64
	detail       string
	stepOrder    int
	percentOrder int
	detailOrder  int
}

type screenshotProgressState struct {
	bootstrapMarker     *screenshotProgressMarker
	subtitleMarker      *screenshotProgressMarker
	prepMarker          *screenshotProgressMarker
	packageMarker       *screenshotProgressMarker
	renderMarker        *screenshotProgressMarker
	captureStarted      int
	captureCompleted    int
	captureTotal        int
	captureStartDetail  string
	captureFinishDetail string
	captureStartOrder   int
	captureFinishOrder  int
	uploadTotal         int
	uploadProcessed     int
	uploadFinished      bool
}

// buildInfoTaskProgress 会根据任务类型、状态和日志推导信息类任务当前进度。
func buildInfoTaskProgress(kind, status string, entries []transport.LogEntry) *transport.TaskProgress {
	if kind == infoKindMediaInfo {
		return nil
	}
	running := estimateInfoTaskRunningProgress(kind, entries)
	switch status {
	case infoJobStatusSucceeded:
		return progressSnapshot(100, "已完成", "任务执行完成。", 0, 0, false)
	case infoJobStatusFailed:
		return finalizeProgress(running, "已失败", "任务执行失败。", false)
	case infoJobStatusCanceled:
		return finalizeProgress(running, "已取消", "任务已取消。", false)
	case infoJobStatusCanceling:
		return progressSnapshot(maxFloat(progressPercent(running), 10), "正在停止", "任务取消中...", progressCurrent(running), progressTotal(running), true)
	case infoJobStatusRunning:
		return running
	case infoJobStatusPending:
		fallthrough
	default:
		return progressSnapshot(6, "等待开始", "任务已提交，等待执行。", 0, 0, true)
	}
}

// buildScreenshotTaskProgress 会根据截图任务模式、状态和日志推导当前进度。
func buildScreenshotTaskProgress(mode, status string, count int, entries []transport.LogEntry) *transport.TaskProgress {
	running := estimateScreenshotTaskRunningProgress(mode, count, entries)
	switch status {
	case screenshotJobStatusSucceeded:
		return progressSnapshot(100, "已完成", "任务执行完成。", 0, 0, false)
	case screenshotJobStatusFailed:
		return finalizeProgress(running, "已失败", "任务执行失败。", false)
	case screenshotJobStatusCanceled:
		return finalizeProgress(running, "已取消", "任务已取消。", false)
	case screenshotJobStatusCanceling:
		return progressSnapshot(maxFloat(progressPercent(running), 10), "正在停止", "任务取消中...", progressCurrent(running), progressTotal(running), true)
	case screenshotJobStatusRunning:
		return running
	case screenshotJobStatusPending:
		fallthrough
	default:
		return progressSnapshot(0, "等待开始", "任务已提交，等待执行。", 0, 0, true)
	}
}

// estimateInfoTaskRunningProgress 会根据任务种类分派到具体的信息任务进度估算器。
func estimateInfoTaskRunningProgress(kind string, entries []transport.LogEntry) *transport.TaskProgress {
	switch kind {
	case infoKindBDInfo:
		return estimateBDInfoRunningProgress(entries)
	case infoKindMediaInfo:
		fallthrough
	default:
		return estimateMediaInfoRunningProgress(entries)
	}
}

// estimateMediaInfoRunningProgress 会根据 MediaInfo 日志阶段估算当前运行进度。
func estimateMediaInfoRunningProgress(entries []transport.LogEntry) *transport.TaskProgress {
	totalCandidates := 0
	currentAttempt := 0
	seenInput := false
	seenBinary := false

	for _, entry := range entries {
		line := strings.TrimSpace(entry.Message)
		switch {
		case strings.HasPrefix(line, "[mediainfo] 输入路径:"):
			seenInput = true
		case strings.HasPrefix(line, "[mediainfo] 使用命令:"):
			seenBinary = true
		case mediainfoCandidatesPattern.MatchString(line):
			matches := mediainfoCandidatesPattern.FindStringSubmatch(line)
			totalCandidates = parseInt(matches, 1)
		case mediainfoAttemptPattern.MatchString(line):
			matches := mediainfoAttemptPattern.FindStringSubmatch(line)
			currentAttempt = parseInt(matches, 1)
			if total := parseInt(matches, 2); total > 0 {
				totalCandidates = total
			}
		}
	}

	switch {
	case totalCandidates > 0 && currentAttempt > 0:
		processed := maxInt(currentAttempt-1, 0)
		percent := 34 + scaledProgress(processed, totalCandidates, 48)
		return progressSnapshot(percent, "分析媒体信息", fmt.Sprintf("正在处理候选源 %d/%d。", currentAttempt, totalCandidates), currentAttempt, totalCandidates, false)
	case totalCandidates > 0:
		return progressSnapshot(28, "准备候选源", fmt.Sprintf("已发现 %d 个候选源。", totalCandidates), 0, totalCandidates, false)
	case seenBinary:
		return progressSnapshot(18, "检查运行环境", "已找到 MediaInfo 可执行文件。", 0, 0, true)
	case seenInput:
		return progressSnapshot(10, "解析输入源", "正在准备候选媒体源。", 0, 0, true)
	default:
		return progressSnapshot(8, "启动中", "正在初始化 MediaInfo 任务。", 0, 0, true)
	}
}

// estimateBDInfoRunningProgress 会根据 BDInfo 扫描日志估算当前运行进度。
func estimateBDInfoRunningProgress(entries []transport.LogEntry) *transport.TaskProgress {
	seenResolvedPath := false
	seenBinary := false
	seenPreparedSource := false
	seenExec := false
	seenAnalyze := false
	seenScanStart := false
	scanPercent := 0
	scanDetail := ""
	seenGenerateReport := false
	seenReportSaved := false
	seenReport := false
	seenMode := false

	for _, entry := range entries {
		line := strings.TrimSpace(entry.Message)
		switch {
		case strings.HasPrefix(line, "[bdinfo] 实际检测路径:"):
			seenResolvedPath = true
		case strings.HasPrefix(line, "[bdinfo] 使用命令:"):
			seenBinary = true
		case strings.Contains(line, "包装 BDMV 根") || strings.Contains(line, "包装输入目录"):
			seenPreparedSource = true
		case strings.HasPrefix(line, "[bdinfo] 执行命令:"):
			seenExec = true
		case strings.HasPrefix(line, "[bdinfo][stdout] Preparing to analyze the following:"):
			seenAnalyze = true
		case strings.HasPrefix(line, "[bdinfo][stdout] Please wait while we scan the disc..."):
			seenScanStart = true
		case bdinfoScanProgressPattern.MatchString(line):
			matches := bdinfoScanProgressPattern.FindStringSubmatch(line)
			scanPercent = parseInt(matches, 1)
			scanDetail = strings.TrimSpace(matches[2])
			seenScanStart = true
		case strings.HasPrefix(line, "[bdinfo][stdout] Please wait while we generate the report..."):
			seenGenerateReport = true
		case strings.HasPrefix(line, "[bdinfo][stdout] Report saved to:"):
			seenReportSaved = true
		case strings.HasPrefix(line, "[bdinfo] 输出报告:"):
			seenReport = true
		case strings.HasPrefix(line, "[bdinfo] 输出模式:"):
			seenMode = true
		}
	}

	switch {
	case seenMode:
		return progressSnapshot(98, "整理结果", "正在按所选模式整理报告内容。", 5, 5, false)
	case seenReport || seenReportSaved:
		return progressSnapshot(95, "读取报告", "已生成报告文件，正在读取结果。", 4, 5, false)
	case seenGenerateReport:
		return progressSnapshot(88, "生成报告", "BDInfo 已完成扫描，正在生成报告。", 4, 5, true)
	case scanPercent > 0:
		percent := 28.0 + scaledProgress(scanPercent, 100, 54)
		detail := "BDInfo 正在扫描目录内容。"
		if scanDetail != "" {
			detail = fmt.Sprintf("正在扫描蓝光目录：%s", scanDetail)
		}
		return progressSnapshot(percent, "扫描蓝光目录", detail, scanPercent, 100, false)
	case seenScanStart:
		return progressSnapshot(28, "扫描蓝光目录", "BDInfo 已启动扫描，正在读取蓝光文件。", 0, 100, true)
	case seenAnalyze:
		return progressSnapshot(24, "分析播放列表", "BDInfo 正在准备分析播放列表。", 2, 5, true)
	case seenExec:
		return progressSnapshot(22, "启动扫描", "BDInfo 命令已启动，等待扫描进度输出。", 2, 5, true)
	case seenPreparedSource:
		return progressSnapshot(16, "准备扫描目录", "已准备好 BDInfo 扫描目录。", 1, 5, false)
	case seenBinary:
		return progressSnapshot(10, "检查运行环境", "已找到 BDInfo 可执行文件。", 1, 5, false)
	case seenResolvedPath:
		return progressSnapshot(4, "解析输入源", "正在准备 BDInfo 实际检测路径。", 0, 5, true)
	default:
		return progressSnapshot(0, "启动中", "正在初始化 BDInfo 任务。", 0, 0, true)
	}
}

// estimateScreenshotTaskRunningProgress 会根据截图模式和日志推导截图任务的运行进度。
func estimateScreenshotTaskRunningProgress(mode string, count int, entries []transport.LogEntry) *transport.TaskProgress {
	requestedCount := screenshot.NormalizeCount(strconv.Itoa(count))
	if requestedCount <= 0 {
		requestedCount = 1
	}

	state := parseScreenshotProgressState(entries)
	if mode == screenshot.ModeLinks {
		if progress := estimateUploadProgressFromMarkers(requestedCount, state); progress != nil {
			return progress
		}
	} else if progress := estimateZipProgressFromMarkers(requestedCount, state); progress != nil {
		return progress
	}

	initFinished := false
	captureAttempts := 0
	captureFinished := false
	uploadTotal := 0
	uploadProcessed := 0
	uploadFinished := false

	for _, entry := range entries {
		line := strings.TrimSpace(entry.Message)
		switch {
		case strings.HasPrefix(line, "[信息] 容器起始偏移："):
			initFinished = true
		case strings.HasPrefix(line, "[信息] 截图:"):
			captureAttempts++
		case line == "===== 任务完成 =====":
			captureFinished = true
		case screenshotUploadStartPattern.MatchString(line):
			matches := screenshotUploadStartPattern.FindStringSubmatch(line)
			uploadTotal = parseInt(matches, 1)
		case strings.HasPrefix(line, "已上传并校准域名:") || strings.HasPrefix(line, "上传失败:"):
			uploadProcessed++
		case strings.HasPrefix(line, "处理完成! 成功:"):
			uploadFinished = true
		}
	}

	if mode == screenshot.ModeLinks {
		return estimateUploadRunningProgress(requestedCount, initFinished, captureAttempts, captureFinished, uploadTotal, uploadProcessed, uploadFinished)
	}
	return estimateZipRunningProgress(requestedCount, initFinished, captureAttempts, captureFinished)
}

// parseScreenshotProgressState 会把截图日志解析成便于估算进度的状态快照。
func parseScreenshotProgressState(entries []transport.LogEntry) screenshotProgressState {
	state := screenshotProgressState{}

	for idx, entry := range entries {
		line := strings.TrimSpace(entry.Message)
		if matches := screenshotPercentPattern.FindStringSubmatch(line); len(matches) == 4 {
			switch strings.TrimSpace(matches[1]) {
			case "启动":
				state.bootstrapMarker = updateScreenshotProgressMarkerPercent(state.bootstrapMarker, parseFloat(matches, 2), strings.TrimSpace(matches[3]), idx)
			case "渲染":
				state.renderMarker = updateScreenshotProgressMarkerPercent(state.renderMarker, parseFloat(matches, 2), strings.TrimSpace(matches[3]), idx)
			case "准备":
				state.prepMarker = updateScreenshotProgressMarkerPercent(state.prepMarker, parseFloat(matches, 2), strings.TrimSpace(matches[3]), idx)
			case "整理":
				state.packageMarker = updateScreenshotProgressMarkerPercent(state.packageMarker, parseFloat(matches, 2), strings.TrimSpace(matches[3]), idx)
			case "字幕":
				state.subtitleMarker = updateScreenshotProgressMarkerPercent(state.subtitleMarker, parseFloat(matches, 2), strings.TrimSpace(matches[3]), idx)
			}
			continue
		}

		if matches := screenshotProgressPattern.FindStringSubmatch(line); len(matches) == 5 {
			current := parseInt(matches, 2)
			total := parseInt(matches, 3)
			detail := strings.TrimSpace(matches[4])
			switch strings.TrimSpace(matches[1]) {
			case "启动":
				state.bootstrapMarker = updateScreenshotProgressMarkerStep(state.bootstrapMarker, current, total, detail, idx)
			case "字幕":
				state.subtitleMarker = updateScreenshotProgressMarkerStep(state.subtitleMarker, current, total, detail, idx)
			case "准备":
				state.prepMarker = updateScreenshotProgressMarkerStep(state.prepMarker, current, total, detail, idx)
			case "截图开始":
				state.captureStarted = current
				state.captureTotal = maxInt(state.captureTotal, total)
				state.captureStartDetail = detail
				state.captureStartOrder = idx
				state.renderMarker = nil
			case "截图完成":
				state.captureCompleted = current
				state.captureTotal = maxInt(state.captureTotal, total)
				state.captureFinishDetail = detail
				state.captureFinishOrder = idx
				state.renderMarker = nil
			case "整理":
				state.packageMarker = updateScreenshotProgressMarkerStep(state.packageMarker, current, total, detail, idx)
			}
			continue
		}

		switch {
		case screenshotUploadStartPattern.MatchString(line):
			matches := screenshotUploadStartPattern.FindStringSubmatch(line)
			state.uploadTotal = parseInt(matches, 1)
		case strings.HasPrefix(line, "已上传并校准域名:") || strings.HasPrefix(line, "上传失败:"):
			state.uploadProcessed++
		case strings.HasPrefix(line, "处理完成! 成功:"):
			state.uploadFinished = true
		}
	}

	return state
}

// estimateZipProgressFromMarkers 会优先根据截图阶段标记估算压缩包模式进度。
func estimateZipProgressFromMarkers(requestedCount int, state screenshotProgressState) *transport.TaskProgress {
	hasSubtitle := state.subtitleMarker != nil
	bootstrapFloor := bootstrapProgressPercent(state.bootstrapMarker)
	if state.packageMarker != nil {
		total := maxInt(state.packageMarker.total, 1)
		effective := markerStepProgress(state.packageMarker, 0.15)
		percent := maxFloat(zipPackageBase(hasSubtitle)+scaledProgressFloat(effective, total, zipPackageWidth()), bootstrapFloor)
		return progressSnapshot(percent, "整理结果", state.packageMarker.detail, state.packageMarker.current, total, true)
	}

	if state.renderMarker != nil || state.captureStarted > 0 || state.captureCompleted > 0 {
		total := maxInt(state.captureTotal, requestedCount)
		effective := float64(state.captureCompleted)
		detail := state.captureFinishDetail
		current := clampInt(maxInt(state.captureCompleted, state.captureStarted), 0, total)
		indeterminate := false
		if state.captureStarted > state.captureCompleted {
			if state.renderMarker != nil && state.renderMarker.percentOrder >= state.captureStartOrder && state.renderMarker.percent > 0 {
				effective += float64(state.renderMarker.percent) / 100.0
				detail = state.renderMarker.detail
				indeterminate = state.renderMarker.percent < 100
			} else {
				effective += 0.1
				detail = state.captureStartDetail
				indeterminate = true
			}
		} else if state.captureCompleted >= total {
			detail = "截图已生成，正在整理结果。"
			current = total
		}
		percent := maxFloat(zipRenderBase(hasSubtitle)+scaledProgressFloat(effective, total, zipRenderWidth(hasSubtitle)), bootstrapFloor)
		return progressSnapshot(percent, "生成截图", detail, current, total, indeterminate)
	}

	if state.prepMarker != nil {
		total := maxInt(state.prepMarker.total, 1)
		effective := markerStageProgress(state.prepMarker, 0.1)
		percent := maxFloat(zipPrepBase(hasSubtitle)+scaledProgressFloat(effective, total, zipPrepWidth(hasSubtitle)), bootstrapFloor)
		return progressSnapshot(percent, "准备截图", state.prepMarker.detail, state.prepMarker.current, total, state.prepMarker.percent <= 0)
	}

	if state.subtitleMarker != nil {
		return progressSnapshot(maxFloat(subtitleProgressPercent(state.subtitleMarker), bootstrapFloor), "准备字幕", state.subtitleMarker.detail, state.subtitleMarker.current, state.subtitleMarker.total, state.subtitleMarker.percent <= 0)
	}

	if state.bootstrapMarker != nil {
		total := maxInt(state.bootstrapMarker.total, 1)
		return progressSnapshot(bootstrapFloor, "准备任务", state.bootstrapMarker.detail, state.bootstrapMarker.current, total, state.bootstrapMarker.percent <= 0)
	}

	return nil
}

// estimateUploadProgressFromMarkers 会优先根据截图阶段标记估算图床上传模式进度。
func estimateUploadProgressFromMarkers(requestedCount int, state screenshotProgressState) *transport.TaskProgress {
	hasSubtitle := state.subtitleMarker != nil
	bootstrapFloor := bootstrapProgressPercent(state.bootstrapMarker)
	if state.uploadFinished {
		processed := state.uploadProcessed
		if state.uploadTotal > 0 {
			processed = clampInt(processed, 0, state.uploadTotal)
		}
		return progressSnapshot(97, "整理图床结果", "上传已完成，正在整理图床链接。", processed, state.uploadTotal, true)
	}

	if state.uploadTotal > 0 {
		processed := clampInt(state.uploadProcessed, 0, state.uploadTotal)
		percent := maxFloat(uploadStageBase()+scaledProgress(processed, state.uploadTotal, uploadStageWidth()), bootstrapFloor)
		return progressSnapshot(percent, "上传图床", fmt.Sprintf("已处理 %d/%d 张截图上传。", processed, state.uploadTotal), processed, state.uploadTotal, false)
	}

	if state.renderMarker != nil || state.captureStarted > 0 || state.captureCompleted > 0 {
		total := maxInt(state.captureTotal, requestedCount)
		effective := float64(state.captureCompleted)
		detail := state.captureFinishDetail
		current := clampInt(maxInt(state.captureCompleted, state.captureStarted), 0, total)
		indeterminate := false
		if state.captureStarted > state.captureCompleted {
			if state.renderMarker != nil && state.renderMarker.percentOrder >= state.captureStartOrder && state.renderMarker.percent > 0 {
				effective += float64(state.renderMarker.percent) / 100.0
				detail = state.renderMarker.detail
				indeterminate = state.renderMarker.percent < 100
			} else {
				effective += 0.1
				detail = state.captureStartDetail
				indeterminate = true
			}
		} else if state.captureCompleted >= total {
			detail = "截图已生成，正在准备上传图床。"
			current = total
		}
		percent := maxFloat(uploadRenderBase(hasSubtitle)+scaledProgressFloat(effective, total, uploadRenderWidth(hasSubtitle)), bootstrapFloor)
		return progressSnapshot(percent, "生成截图", detail, current, total, indeterminate)
	}

	if state.prepMarker != nil {
		total := maxInt(state.prepMarker.total, 1)
		effective := markerStageProgress(state.prepMarker, 0.1)
		percent := maxFloat(uploadPrepBase(hasSubtitle)+scaledProgressFloat(effective, total, uploadPrepWidth(hasSubtitle)), bootstrapFloor)
		return progressSnapshot(percent, "准备截图", state.prepMarker.detail, state.prepMarker.current, total, state.prepMarker.percent <= 0)
	}

	if state.subtitleMarker != nil {
		return progressSnapshot(maxFloat(subtitleProgressPercent(state.subtitleMarker), bootstrapFloor), "准备字幕", state.subtitleMarker.detail, state.subtitleMarker.current, state.subtitleMarker.total, state.subtitleMarker.percent <= 0)
	}

	if state.bootstrapMarker != nil {
		total := maxInt(state.bootstrapMarker.total, 1)
		return progressSnapshot(bootstrapFloor, "准备任务", state.bootstrapMarker.detail, state.bootstrapMarker.current, total, state.bootstrapMarker.percent <= 0)
	}

	return nil
}

// updateScreenshotProgressMarkerStep 会用 step 型进度日志刷新阶段标记。
func updateScreenshotProgressMarkerStep(marker *screenshotProgressMarker, current, total int, detail string, order int) *screenshotProgressMarker {
	if marker == nil {
		marker = &screenshotProgressMarker{}
	}
	marker.current = current
	marker.total = total
	marker.percent = 0
	marker.stepOrder = order
	if order >= marker.detailOrder {
		marker.detail = detail
		marker.detailOrder = order
	}
	return marker
}

// updateScreenshotProgressMarkerPercent 会用百分比型进度日志刷新阶段标记。
func updateScreenshotProgressMarkerPercent(marker *screenshotProgressMarker, percent float64, detail string, order int) *screenshotProgressMarker {
	if marker == nil {
		marker = &screenshotProgressMarker{}
	}
	if percent > marker.percent {
		marker.percent = percent
	}
	marker.percentOrder = order
	if order >= marker.detailOrder {
		marker.detail = detail
		marker.detailOrder = order
	}
	return marker
}

// markerStepProgress 会把阶段 step 进度换算成连续的有效进度值。
func markerStepProgress(marker *screenshotProgressMarker, entryBias float64) float64 {
	if marker == nil {
		return 0
	}
	if marker.current <= 0 {
		return 0
	}
	return float64(marker.current) - entryBias
}

// markerStageProgress 会综合 step 和百分比标记换算阶段内的连续进度值。
func markerStageProgress(marker *screenshotProgressMarker, entryBias float64) float64 {
	if marker == nil {
		return 0
	}
	if marker.current > 0 {
		effective := float64(maxInt(marker.current-1, 0))
		if marker.percentOrder >= marker.stepOrder && marker.percent > 0 {
			return effective + float64(marker.percent)/100.0
		}
		return effective + entryBias
	}
	if marker.percent > 0 && marker.total > 0 {
		return float64(marker.percent) / 100.0 * float64(marker.total)
	}
	if marker.percent > 0 {
		return float64(marker.percent) / 100.0
	}
	return 0
}

// subtitleProgressPercent 会把字幕准备阶段标记换算为整体任务百分比。
func subtitleProgressPercent(marker *screenshotProgressMarker) float64 {
	if marker == nil {
		return 0
	}
	if marker.total > 0 && marker.current > 0 {
		return clampPercent(markerStageProgress(marker, 0.1) / float64(marker.total) * float64(subtitleStageWidth()))
	}
	if marker.percent <= 0 {
		return 0
	}
	return clampPercent(clampPercent(marker.percent) / 100.0 * float64(subtitleStageWidth()))
}

// bootstrapProgressPercent 会把截图启动阶段标记换算为整体任务前段的百分比。
func bootstrapProgressPercent(marker *screenshotProgressMarker) float64 {
	if marker == nil {
		return 0
	}
	if marker.total > 0 && marker.current > 0 {
		return clampPercent(markerStageProgress(marker, 0.1) / float64(marker.total) * 8)
	}
	if marker.percent <= 0 {
		return 0
	}
	return clampPercent(clampPercent(marker.percent) / 100.0 * 8)
}

// estimateZipRunningProgress 会在缺少细粒度标记时估算压缩包模式的粗略进度。
func estimateZipRunningProgress(requestedCount int, initFinished bool, captureAttempts int, captureFinished bool) *transport.TaskProgress {
	if !initFinished {
		return progressSnapshot(0, "准备任务", "正在等待耗时步骤开始。", 0, 0, true)
	}
	if captureFinished {
		return progressSnapshot(90, "打包结果", "截图已生成，正在整理下载包。", requestedCount, requestedCount, true)
	}

	processed := clampInt(captureAttempts, 0, requestedCount)
	percent := scaledProgress(processed, requestedCount, 100)
	return progressSnapshot(percent, "生成截图", fmt.Sprintf("已处理 %d/%d 个截图点。", processed, requestedCount), processed, requestedCount, false)
}

// estimateUploadRunningProgress 会在缺少细粒度标记时估算上传模式的粗略进度。
func estimateUploadRunningProgress(requestedCount int, initFinished bool, captureAttempts int, captureFinished bool, uploadTotal int, uploadProcessed int, uploadFinished bool) *transport.TaskProgress {
	if !initFinished {
		return progressSnapshot(0, "准备任务", "正在等待耗时步骤开始。", 0, 0, true)
	}

	if uploadFinished {
		processed := uploadProcessed
		if uploadTotal > 0 {
			processed = clampInt(processed, 0, uploadTotal)
		}
		return progressSnapshot(97, "整理图床结果", "上传已完成，正在整理图床链接。", processed, uploadTotal, true)
	}

	if uploadTotal > 0 {
		processed := clampInt(uploadProcessed, 0, uploadTotal)
		percent := uploadStageBase() + scaledProgress(processed, uploadTotal, uploadStageWidth())
		return progressSnapshot(percent, "上传图床", fmt.Sprintf("已处理 %d/%d 张截图上传。", processed, uploadTotal), processed, uploadTotal, false)
	}

	if captureFinished {
		return progressSnapshot(uploadStageBase(), "准备上传", "截图已生成，正在准备上传图床。", requestedCount, requestedCount, true)
	}

	processed := clampInt(captureAttempts, 0, requestedCount)
	percent := scaledProgress(processed, requestedCount, uploadRenderWidth(false))
	return progressSnapshot(percent, "生成截图", fmt.Sprintf("已处理 %d/%d 个截图点。", processed, requestedCount), processed, requestedCount, false)
}

// subtitleStageWidth 会返回字幕准备阶段在总进度中的宽度占比。
func subtitleStageWidth() int {
	return 30
}

// zipRenderBase 会返回压缩包模式中渲染阶段的起始百分比。
func zipRenderBase(hasSubtitle bool) float64 {
	if hasSubtitle {
		return 35
	}
	return 0
}

// zipRenderWidth 会返回压缩包模式中渲染阶段的百分比宽度。
func zipRenderWidth(hasSubtitle bool) int {
	if hasSubtitle {
		return 55
	}
	return 90
}

// zipPrepBase 会返回压缩包模式中准备阶段的起始百分比。
func zipPrepBase(hasSubtitle bool) float64 {
	if hasSubtitle {
		return 30
	}
	return 0
}

// zipPrepWidth 会返回压缩包模式中准备阶段的百分比宽度。
func zipPrepWidth(hasSubtitle bool) int {
	if hasSubtitle {
		return 5
	}
	return 0
}

// zipPackageBase 会返回压缩包模式中整理阶段的起始百分比。
func zipPackageBase(hasSubtitle bool) float64 {
	if hasSubtitle {
		return 90
	}
	return 90
}

// zipPackageWidth 会返回压缩包模式中整理阶段的百分比宽度。
func zipPackageWidth() int {
	return 10
}

// uploadRenderBase 会返回上传模式中渲染阶段的起始百分比。
func uploadRenderBase(hasSubtitle bool) float64 {
	if hasSubtitle {
		return 35
	}
	return 0
}

// uploadRenderWidth 会返回上传模式中渲染阶段的百分比宽度。
func uploadRenderWidth(hasSubtitle bool) int {
	if hasSubtitle {
		return 35
	}
	return 70
}

// uploadPrepBase 会返回上传模式中准备阶段的起始百分比。
func uploadPrepBase(hasSubtitle bool) float64 {
	if hasSubtitle {
		return 30
	}
	return 0
}

// uploadPrepWidth 会返回上传模式中准备阶段的百分比宽度。
func uploadPrepWidth(hasSubtitle bool) int {
	if hasSubtitle {
		return 5
	}
	return 0
}

// uploadStageBase 会返回上传阶段在整体任务中的起始百分比。
func uploadStageBase() float64 {
	return 70
}

// uploadStageWidth 会返回上传阶段在整体任务中的百分比宽度。
func uploadStageWidth() int {
	return 30
}

// finalizeProgress 会基于当前快照生成任务结束态的进度结果。
func finalizeProgress(base *transport.TaskProgress, stage, detail string, indeterminate bool) *transport.TaskProgress {
	if base == nil {
		return progressSnapshot(100, stage, detail, 0, 0, indeterminate)
	}

	return progressSnapshot(
		maxFloat(base.Percent, 1),
		stage,
		detail,
		base.Current,
		base.Total,
		indeterminate,
	)
}

// progressSnapshot 会构造一个标准化的任务进度对象。
func progressSnapshot(percent float64, stage, detail string, current, total int, indeterminate bool) *transport.TaskProgress {
	progress := &transport.TaskProgress{
		Percent:       clampPercent(percent),
		Stage:         stage,
		Detail:        detail,
		Indeterminate: indeterminate,
	}
	if current > 0 {
		progress.Current = current
	}
	if total > 0 {
		progress.Total = total
	}
	return progress
}

// scaledProgress 会把整数进度映射到指定宽度的百分比区间。
func scaledProgress(current, total, width int) float64 {
	if total <= 0 || width <= 0 {
		return 0
	}
	boundedCurrent := clampInt(current, 0, total)
	return float64(boundedCurrent) / float64(total) * float64(width)
}

// scaledProgressFloat 会把浮点进度映射到指定宽度的百分比区间。
func scaledProgressFloat(current float64, total, width int) float64 {
	if total <= 0 || width <= 0 {
		return 0
	}
	if current < 0 {
		current = 0
	}
	maxCurrent := float64(total)
	if current > maxCurrent {
		current = maxCurrent
	}
	return current / float64(total) * float64(width)
}

// clampInt 会把整数限制在给定闭区间内。
func clampInt(value, minValue, maxValue int) int {
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

// clampPercent 会把百分比限制在 0-100 区间内。
func clampPercent(value float64) float64 {
	switch {
	case value < 0:
		return 0
	case value > 100:
		return 100
	default:
		return value
	}
}

// parseInt 会从正则匹配结果中安全读取指定索引的整数值。
func parseInt(values []string, index int) int {
	if index < 0 || index >= len(values) {
		return 0
	}
	value, err := strconv.Atoi(strings.TrimSpace(values[index]))
	if err != nil {
		return 0
	}
	return value
}

// parseFloat 会从正则匹配结果中安全读取指定索引的浮点值。
func parseFloat(values []string, index int) float64 {
	if index < 0 || index >= len(values) {
		return 0
	}
	value, err := strconv.ParseFloat(strings.TrimSpace(values[index]), 64)
	if err != nil {
		return 0
	}
	return value
}

// progressPercent 会安全读取进度对象中的百分比值。
func progressPercent(progress *transport.TaskProgress) float64 {
	if progress == nil {
		return 0
	}
	return progress.Percent
}

// progressCurrent 会安全读取进度对象中的当前计数。
func progressCurrent(progress *transport.TaskProgress) int {
	if progress == nil {
		return 0
	}
	return progress.Current
}

// progressTotal 会安全读取进度对象中的总计数。
func progressTotal(progress *transport.TaskProgress) int {
	if progress == nil {
		return 0
	}
	return progress.Total
}

// maxInt 会返回两个整数中的较大值。
func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}

// maxFloat 会返回两个浮点数中的较大值。
func maxFloat(left, right float64) float64 {
	if left > right {
		return left
	}
	return right
}
