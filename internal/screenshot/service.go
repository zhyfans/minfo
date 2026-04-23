// Package screenshot 对外暴露截图与上传服务入口。

package screenshot

import "context"

// RunScreenshots 执行截图流程并仅返回生成的文件列表。
func RunScreenshots(ctx context.Context, inputPath, outputDir, variant, subtitleMode string, count int) ([]string, error) {
	result, err := RunScreenshotsWithLogs(ctx, inputPath, outputDir, variant, subtitleMode, count)
	if err != nil {
		return nil, err
	}
	return result.Files, nil
}

// RunScreenshotsWithLogs 执行截图流程并返回文件列表与完整日志。
func RunScreenshotsWithLogs(ctx context.Context, inputPath, outputDir, variant, subtitleMode string, count int) (ScreenshotsResult, error) {
	return RunScreenshotsWithLiveLogs(ctx, inputPath, outputDir, variant, subtitleMode, count, nil)
}

// RunScreenshotsWithLiveLogs 会执行截图流程，并把实时日志通过回调逐行暴露给调用方。
func RunScreenshotsWithLiveLogs(ctx context.Context, inputPath, outputDir, variant, subtitleMode string, count int, onLog LogHandler) (ScreenshotsResult, error) {
	return runEngineScreenshotsWithLiveLogs(ctx, inputPath, outputDir, variant, subtitleMode, count, onLog)
}

// RunUpload 执行截图加上传流程并仅返回直链输出。
func RunUpload(ctx context.Context, inputPath, outputDir, variant, subtitleMode string, count int) (string, error) {
	result, err := RunUploadWithLogs(ctx, inputPath, outputDir, variant, subtitleMode, count)
	if err != nil {
		return "", err
	}
	return result.Output, nil
}

// RunUploadWithLogs 执行截图加上传流程并返回直链输出与完整日志。
func RunUploadWithLogs(ctx context.Context, inputPath, outputDir, variant, subtitleMode string, count int) (UploadResult, error) {
	return RunUploadWithLiveLogs(ctx, inputPath, outputDir, variant, subtitleMode, count, nil)
}

// RunUploadWithLiveLogs 会执行截图加上传流程，并把实时日志通过回调逐行暴露给调用方。
func RunUploadWithLiveLogs(ctx context.Context, inputPath, outputDir, variant, subtitleMode string, count int, onLog LogHandler) (UploadResult, error) {
	return RunUploadWithLiveEvents(ctx, inputPath, outputDir, variant, subtitleMode, count, onLog, nil)
}

// RunUploadWithLiveEvents 会执行截图加上传流程，并把实时日志和已完成图片逐步暴露给调用方。
func RunUploadWithLiveEvents(ctx context.Context, inputPath, outputDir, variant, subtitleMode string, count int, onLog LogHandler, onItem UploadItemHandler) (UploadResult, error) {
	return runPixhostUploadWithLiveLogs(ctx, inputPath, outputDir, variant, subtitleMode, count, onLog, onItem)
}
