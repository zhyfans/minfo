// Package screenshot 负责截图完成后的图床上传编排。

package screenshot

import (
	"context"
	"strings"

	screenshotpixhost "minfo/internal/screenshot/pixhost"
)

// runPixhostUploadWithLiveLogs 会先生成截图，再把图片上传到 Pixhost，并合并两阶段日志。
func runPixhostUploadWithLiveLogs(ctx context.Context, inputPath, outputDir, variant, subtitleMode string, count int, onLog LogHandler, onItem UploadItemHandler) (UploadResult, error) {
	screenshotResult, err := runEngineScreenshotsWithLiveLogs(ctx, inputPath, outputDir, variant, subtitleMode, count, onLog)
	if err != nil {
		return UploadResult{Logs: screenshotResult.Logs}, err
	}

	uploadResult, err := screenshotpixhost.UploadImages(ctx, screenshotResult.Files, screenshotResult.LossyPNGFiles, oversizeBytes, onLog, onItem)
	logs := mergePixhostUploadLogs(screenshotResult.Logs, uploadResult.Logs)
	if err != nil {
		return UploadResult{
			Logs:            logs,
			Items:           uploadResult.Items,
			LossyPNGFiles:   screenshotResult.LossyPNGFiles,
			LossyPNGIndexes: uploadResult.LossyIndexes,
		}, err
	}
	return UploadResult{
		Output:          uploadResult.Output,
		Logs:            logs,
		Items:           uploadResult.Items,
		LossyPNGFiles:   screenshotResult.LossyPNGFiles,
		LossyPNGIndexes: uploadResult.LossyIndexes,
	}, nil
}

// mergePixhostUploadLogs 会把截图阶段和上传阶段日志按原顺序拼接成最终输出。
func mergePixhostUploadLogs(screenshotLogs, uploadLogs string) string {
	parts := make([]string, 0, 2)
	if strings.TrimSpace(screenshotLogs) != "" {
		parts = append(parts, strings.TrimSpace(screenshotLogs))
	}
	if strings.TrimSpace(uploadLogs) != "" {
		parts = append(parts, strings.TrimSpace(uploadLogs))
	}
	return strings.TrimSpace(strings.Join(parts, "\n\n"))
}
