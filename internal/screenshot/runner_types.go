// Package screenshot 定义截图运行器的核心状态结构。

package screenshot

import "context"

type screenshotRunner struct {
	ctx              context.Context
	sourcePath       string
	dvdMediaInfoPath string
	outputDir        string
	variant          string
	subtitleMode     string
	requested        []float64
	settings         variantSettings
	tools            runtimeToolchain
	logger           runtimeLogger
	lossyPNGFiles    map[string]struct{}
	media            runtimeMediaState
	render           runtimeRenderState
	subtitleState    runtimeSubtitleState

	subtitle subtitleSelection

	activeShot activeShotState
}
