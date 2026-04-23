// Package handlers 提供截图后台任务的执行与状态流转逻辑。

package handlers

import (
	"context"
	"errors"
	"os"
	"strings"
	"time"

	"minfo/internal/config"
	"minfo/internal/httpapi/transport"
	"minfo/internal/screenshot"
)

// run 会在后台执行具体截图任务，并在结束后更新任务状态和结果。
func (j *screenshotJob) run() {
	defer func() {
		if j.cancel != nil {
			j.cancel()
		}
		if j.cleanup != nil {
			j.cleanup()
		}
		if j.logger != nil {
			j.logger.Close()
		}
	}()

	if !j.beginRun() {
		if j.isCancellationRequested() {
			j.finishCanceled()
		}
		return
	}

	ctx, cancel := context.WithTimeout(j.taskContext, config.RequestTimeout)
	defer cancel()

	tempDir, err := createScreenshotTempDir("minfo-screenshot-job-*")
	if err != nil {
		j.fail(err)
		return
	}
	defer os.RemoveAll(tempDir)

	switch j.mode {
	case screenshot.ModeLinks:
		result, err := screenshot.RunUploadWithLiveEvents(
			ctx,
			j.inputPath,
			tempDir,
			j.variant,
			j.subtitleMode,
			j.count,
			j.logger.LogLine,
			func(item screenshot.UploadedImage) {
				j.appendLinkItem(buildTransportImageLinkItem(item))
			},
		)
		if err != nil {
			j.fail(err)
			return
		}
		j.succeed(result.Output, "", buildTransportImageLinkItems(result.Items), result.LossyPNGFiles, result.LossyPNGIndexes)
	default:
		downloadURL, _, err := prepareScreenshotZipDownload(ctx, j.inputPath, tempDir, j.variant, j.subtitleMode, j.count, j.logger.LogLine)
		if err != nil {
			j.fail(err)
			return
		}
		j.succeed("", downloadURL, nil, nil, nil)
	}
}

// snapshot 会生成当前任务的安全快照，供 HTTP 接口直接返回。
func (j *screenshotJob) snapshot() transport.ScreenshotJobResponse {
	j.mu.RLock()
	count := j.count
	response := transport.ScreenshotJobResponse{
		OK:              true,
		JobID:           j.id,
		Status:          j.status,
		Mode:            j.mode,
		Output:          j.output,
		DownloadURL:     j.downloadURL,
		LinkItems:       append([]transport.ImageLinkItem(nil), j.linkItems...),
		Error:           j.errMessage,
		PNGLossyFiles:   append([]string(nil), j.pngLossyFiles...),
		PNGLossyIndexes: append([]int(nil), j.pngLossyIndexes...),
	}
	logger := j.logger
	j.mu.RUnlock()

	var entries []transport.LogEntry
	if logger != nil {
		response.Logs = logger.String()
		entries = logger.Entries()
		response.LogEntries = entries
	}
	response.Progress = buildScreenshotTaskProgress(response.Mode, response.Status, count, response.LogEntries)
	return response
}

// expired 会判断后台任务是否已经完成且超过保留时间。
func (j *screenshotJob) expired(now time.Time) bool {
	j.mu.RLock()
	defer j.mu.RUnlock()

	if j.completedAt.IsZero() {
		return false
	}
	return now.Sub(j.completedAt) > screenshotJobTTL
}

// beginRun 会把任务从 pending 切换到 running；如果任务已被取消，则返回 false。
func (j *screenshotJob) beginRun() bool {
	j.mu.Lock()
	defer j.mu.Unlock()

	if j.status != screenshotJobStatusPending {
		return false
	}
	if j.cancelRequested || errors.Is(j.taskContext.Err(), context.Canceled) {
		return false
	}

	j.status = screenshotJobStatusRunning
	j.updatedAt = time.Now()
	return true
}

// requestCancel 会请求取消当前任务，并立刻把状态推进到 canceling。
func (j *screenshotJob) requestCancel() {
	var cancel context.CancelFunc

	j.mu.Lock()
	switch j.status {
	case screenshotJobStatusSucceeded, screenshotJobStatusFailed, screenshotJobStatusCanceled:
		j.mu.Unlock()
		return
	case screenshotJobStatusCanceling:
		j.mu.Unlock()
		return
	default:
		j.cancelRequested = true
		j.status = screenshotJobStatusCanceling
		j.errMessage = "任务取消中。"
		j.updatedAt = time.Now()
		cancel = j.cancel
		j.mu.Unlock()
	}

	if cancel != nil {
		cancel()
	}
}

// succeed 会记录后台任务成功产出的最终结果。
func (j *screenshotJob) succeed(output, downloadURL string, linkItems []transport.ImageLinkItem, pngLossyFiles []string, pngLossyIndexes []int) {
	j.mu.Lock()
	defer j.mu.Unlock()

	now := time.Now()
	if j.cancelRequested || errors.Is(j.taskContext.Err(), context.Canceled) {
		j.status = screenshotJobStatusCanceled
		j.output = ""
		j.downloadURL = ""
		j.linkItems = nil
		j.pngLossyFiles = nil
		j.pngLossyIndexes = nil
		j.errMessage = "任务已取消。"
		j.updatedAt = now
		j.completedAt = now
		return
	}

	j.status = screenshotJobStatusSucceeded
	j.output = output
	j.downloadURL = downloadURL
	j.linkItems = append([]transport.ImageLinkItem(nil), linkItems...)
	j.pngLossyFiles = append([]string(nil), pngLossyFiles...)
	j.pngLossyIndexes = append([]int(nil), pngLossyIndexes...)
	j.errMessage = ""
	j.updatedAt = now
	j.completedAt = now
}

// fail 会记录后台任务失败原因，并把状态切换为 failed。
func (j *screenshotJob) fail(err error) {
	j.mu.Lock()
	defer j.mu.Unlock()

	now := time.Now()
	if j.cancelRequested || isScreenshotJobCanceledError(err) || errors.Is(j.taskContext.Err(), context.Canceled) {
		j.status = screenshotJobStatusCanceled
		j.output = ""
		j.downloadURL = ""
		j.linkItems = nil
		j.pngLossyFiles = nil
		j.pngLossyIndexes = nil
		j.errMessage = "任务已取消。"
		j.updatedAt = now
		j.completedAt = now
		return
	}

	j.status = screenshotJobStatusFailed
	j.output = ""
	j.downloadURL = ""
	j.linkItems = nil
	j.pngLossyFiles = nil
	j.pngLossyIndexes = nil
	if err != nil {
		j.errMessage = err.Error()
	} else {
		j.errMessage = "job failed"
	}
	j.updatedAt = now
	j.completedAt = now
}

// finishCanceled 会把任务最终标记为 canceled，并记录完成时间。
func (j *screenshotJob) finishCanceled() {
	j.mu.Lock()
	defer j.mu.Unlock()

	if j.status == screenshotJobStatusSucceeded || j.status == screenshotJobStatusFailed || j.status == screenshotJobStatusCanceled {
		return
	}

	now := time.Now()
	j.status = screenshotJobStatusCanceled
	j.output = ""
	j.downloadURL = ""
	j.linkItems = nil
	j.pngLossyFiles = nil
	j.pngLossyIndexes = nil
	j.errMessage = "任务已取消。"
	j.updatedAt = now
	j.completedAt = now
}

// appendLinkItem 会在后台任务运行中逐步追加已完成上传的图片链接。
func (j *screenshotJob) appendLinkItem(item transport.ImageLinkItem) {
	if strings.TrimSpace(item.URL) == "" {
		return
	}

	j.mu.Lock()
	defer j.mu.Unlock()

	for _, existing := range j.linkItems {
		if existing.URL == item.URL {
			return
		}
	}

	j.linkItems = append(j.linkItems, item)
	j.updatedAt = time.Now()
}

// isCancellationRequested 会判断当前任务是否已经收到了取消请求。
func (j *screenshotJob) isCancellationRequested() bool {
	j.mu.RLock()
	defer j.mu.RUnlock()

	return j.cancelRequested || errors.Is(j.taskContext.Err(), context.Canceled)
}

// isScreenshotJobCanceledError 会判断错误是否来自主动取消而不是普通失败。
func isScreenshotJobCanceledError(err error) bool {
	return errors.Is(err, context.Canceled)
}

func buildTransportImageLinkItems(items []screenshot.UploadedImage) []transport.ImageLinkItem {
	if len(items) == 0 {
		return nil
	}

	result := make([]transport.ImageLinkItem, 0, len(items))
	for _, item := range items {
		if normalized := buildTransportImageLinkItem(item); strings.TrimSpace(normalized.URL) != "" {
			result = append(result, normalized)
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func buildTransportImageLinkItem(item screenshot.UploadedImage) transport.ImageLinkItem {
	return transport.ImageLinkItem{
		URL:      strings.TrimSpace(item.URL),
		Filename: item.Filename,
		Size:     item.Size,
	}
}
