package runtime

// RenderState 维护截图流程运行时的渲染参数和字幕画布信息。
type RenderState struct {
	SubtitleCanvasWidth  int
	SubtitleCanvasHeight int
	AspectChain          string
	ColorInfo            string
	ColorChain           string
}
