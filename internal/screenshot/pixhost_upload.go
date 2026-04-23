// Package screenshot 负责 Pixhost 上传流程编排与结果汇总。

package screenshot

import (
	"context"
	"errors"
	"net/http"
	"strings"
)

// runPixhostUploadWithLiveLogs 会先生成截图，再把图片上传到 Pixhost，并合并两阶段日志。
func runPixhostUploadWithLiveLogs(ctx context.Context, inputPath, outputDir, variant, subtitleMode string, count int, onLog LogHandler, onItem UploadItemHandler) (UploadResult, error) {
	screenshotResult, err := runEngineScreenshotsWithLiveLogs(ctx, inputPath, outputDir, variant, subtitleMode, count, onLog)
	if err != nil {
		return UploadResult{Logs: screenshotResult.Logs}, err
	}

	output, uploadLogs, items, lossyIndexes, err := uploadImagesToPixhost(ctx, screenshotResult.Files, screenshotResult.LossyPNGFiles, onLog, onItem)
	logs := mergePixhostUploadLogs(screenshotResult.Logs, uploadLogs)
	if err != nil {
		return UploadResult{
			Logs:            logs,
			Items:           items,
			LossyPNGFiles:   screenshotResult.LossyPNGFiles,
			LossyPNGIndexes: lossyIndexes,
		}, err
	}
	return UploadResult{
		Output:          output,
		Logs:            logs,
		Items:           items,
		LossyPNGFiles:   screenshotResult.LossyPNGFiles,
		LossyPNGIndexes: lossyIndexes,
	}, nil
}

// mergePixhostUploadLogs 会把截图阶段和上传阶段日志按原顺序拼接成最终输出。
func mergePixhostUploadLogs(screenshotLogs, uploadLogs string) string {
	return strings.TrimSpace(strings.Join(filterNonEmptyStrings(screenshotLogs, uploadLogs), "\n\n"))
}

// uploadImagesToPixhost 过滤可上传图片，逐个上传到 Pixhost，并返回整理后的直链文本和日志。
func uploadImagesToPixhost(ctx context.Context, files, lossyFiles []string, onLog LogHandler, onItem UploadItemHandler) (string, string, []UploadedImage, []int, error) {
	images := collectUploadableImages(files)
	batch := newPixhostUploadBatch(lossyFiles, onLog, onItem)
	if len(images) == 0 {
		batch.appendLog("警告: 未找到有效图片文件")
		return "", batch.logs(), nil, nil, errors.New("no uploadable screenshots were found")
	}

	batch.appendLog("开始处理 %d 个文件...", len(images))
	client := &http.Client{}
	apiURL := pixhostAPIEndpoint()
	for _, imagePath := range images {
		directURL, err := uploadSinglePixhostImage(ctx, client, apiURL, imagePath)
		if err != nil {
			batch.recordFailure(imagePath, err)
			continue
		}
		batch.recordSuccess(imagePath, directURL)
	}

	return batch.finalize(len(images))
}
