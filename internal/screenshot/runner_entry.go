// Package screenshot 负责截图入口解析和运行器创建编排。

package screenshot

import (
	"context"

	"minfo/internal/media"
)

// resolvedScreenshotSources 表示截图入口阶段解析出的主媒体源与 DVD 附加探测源。
type resolvedScreenshotSources struct {
	sourcePath       string
	dvdMediaInfoPath string
	cleanup          func()
}

// runEngineScreenshotsWithLiveLogs 会解析输入源、生成随机时间点，并启动带实时日志的截图引擎流程。
func runEngineScreenshotsWithLiveLogs(ctx context.Context, inputPath, outputDir, variant, subtitleMode string, count int, onLog LogHandler) (ScreenshotsResult, error) {
	sources, err := resolveScreenshotSources(ctx, inputPath, onLog)
	if err != nil {
		return ScreenshotsResult{}, err
	}
	defer sources.cleanup()

	timestamps, err := generateScreenshotTimestamps(ctx, sources.sourcePath, count, onLog)
	if err != nil {
		return ScreenshotsResult{}, err
	}

	return runScreenshotsFromSource(ctx, sources.sourcePath, sources.dvdMediaInfoPath, outputDir, variant, subtitleMode, timestamps, onLog)
}

// resolveScreenshotSources 会把外部输入路径解析为截图主媒体源和 DVD 附加探测源。
func resolveScreenshotSources(ctx context.Context, inputPath string, onLog LogHandler) (resolvedScreenshotSources, error) {
	EmitProgressLog(onLog, "启动", 1, 3, "正在解析截图输入源。")
	sourcePath, cleanupSource, err := media.ResolveScreenshotSource(ctx, inputPath)
	if err != nil {
		return resolvedScreenshotSources{}, err
	}

	EmitProgressLog(onLog, "启动", 2, 3, "正在定位 DVD 附加元数据源。")
	dvdMediaInfoPath, cleanupDVD, dvdMediaInfoErr := media.ResolveDVDMediaInfoSource(ctx, inputPath)
	if dvdMediaInfoErr != nil {
		dvdMediaInfoPath = ""
		cleanupDVD = func() {}
	}

	return resolvedScreenshotSources{
		sourcePath:       sourcePath,
		dvdMediaInfoPath: dvdMediaInfoPath,
		cleanup: func() {
			cleanupDVD()
			cleanupSource()
		},
	}, nil
}

// generateScreenshotTimestamps 会在入口阶段输出统一进度，并生成本轮随机截图时间点。
func generateScreenshotTimestamps(ctx context.Context, sourcePath string, count int, onLog LogHandler) ([]string, error) {
	detail := "正在估算影片时长并生成随机截图时间点。"
	EmitProgressLog(onLog, "启动", 3, 3, detail)
	stopHeartbeat := startStandaloneProgressHeartbeat(ctx, onLog, "启动", detail)
	timestamps, err := randomScreenshotTimestampsForSource(ctx, sourcePath, count)
	stopHeartbeat()
	if err != nil {
		return nil, err
	}
	return timestamps, nil
}

// runScreenshotsFromSource 会基于已经解析好的媒体源创建运行器，并执行一次完整截图任务。
func runScreenshotsFromSource(ctx context.Context, sourcePath, dvdMediaInfoPath, outputDir, variant, subtitleMode string, timestamps []string, onLog LogHandler) (ScreenshotsResult, error) {
	runner := newScreenshotRunner(ctx, sourcePath, dvdMediaInfoPath, outputDir, variant, subtitleMode, onLog)
	defer runner.cleanupTemporarySubtitleResources()

	runner.logRuntimeBootstrap()
	if err := runner.init(timestamps); err != nil {
		return ScreenshotsResult{Logs: runner.logs()}, err
	}

	files, err := runner.run()
	if err != nil {
		return ScreenshotsResult{Logs: runner.logs()}, err
	}
	return ScreenshotsResult{
		Files:         files,
		Logs:          runner.logs(),
		LossyPNGFiles: runner.lossyPNGFileList(),
	}, nil
}

// newScreenshotRunner 会基于入口参数创建一份新的截图运行器。
func newScreenshotRunner(ctx context.Context, sourcePath, dvdMediaInfoPath, outputDir, variant, subtitleMode string, onLog LogHandler) *screenshotRunner {
	return &screenshotRunner{
		ctx:              ctx,
		sourcePath:       sourcePath,
		dvdMediaInfoPath: dvdMediaInfoPath,
		outputDir:        outputDir,
		variant:          NormalizeVariant(variant),
		subtitleMode:     NormalizeSubtitleMode(subtitleMode),
		settings:         variantSettingsFor(variant),
		subtitle: subtitleSelection{
			Mode: "none",
		},
		logger:        newRuntimeLogger(onLog),
		lossyPNGFiles: make(map[string]struct{}),
	}
}

// logRuntimeBootstrap 会输出运行器切换和 DVD 选片等启动摘要日志。
func (r *screenshotRunner) logRuntimeBootstrap() {
	r.logf("[信息] 已切换为 Go 截图引擎。")
	if !looksLikeDVDSource(r.dvdProbeSource()) {
		return
	}

	r.logf("[信息] DVD 已选片段：VOB=%s | IFO=%s",
		displayProbeValue(r.dvdSelectedVOBPath()),
		displayProbeValue(r.dvdSelectedIFOPath()),
	)
}
