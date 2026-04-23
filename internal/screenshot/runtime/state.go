// Package runtime 定义截图运行期在不同子模块间共享的状态容器。
package runtime

// Toolchain 维护截图流程运行时依赖的外部二进制路径和渲染能力开关。
type Toolchain struct {
	FFmpegBin       string
	FFprobeBin      string
	MediaInfoBin    string
	OxiPNGBin       string
	PNGQuantBin     string
	BDSubBin        string
	LibplaceboReady bool
}

// MediaState 维护截图流程运行时的媒体时间轴和显示尺寸信息。
type MediaState struct {
	StartOffset   float64
	Duration      float64
	VideoWidth    int
	VideoHeight   int
	DisplayWidth  int
	DisplayHeight int
}

// RenderState 维护截图流程运行时的渲染参数和字幕画布信息。
type RenderState struct {
	SubtitleCanvasWidth  int
	SubtitleCanvasHeight int
	AspectChain          string
	ColorInfo            string
	ColorChain           string
}

// DVDMediaInfoTrack 表示 mediainfo 探测到的一条 DVD 字幕轨。
type DVDMediaInfoTrack struct {
	StreamID int
	ID       string
	Format   string
	Language string
	Title    string
	Source   string
}

// DVDMediaInfoResult 表示一次 DVD mediainfo 探测返回的结果。
type DVDMediaInfoResult struct {
	Duration             float64
	DisplayAspectRatio   string
	Tracks               []DVDMediaInfoTrack
	ProbePath            string
	SelectedVOBPath      string
	LanguageFallbackPath string
}

// SubtitleState 维护截图流程运行时的字幕索引、缓存和补充元数据。
type SubtitleState struct {
	BlurayContext            BlurayProbeContext
	Index                    []SubtitleSpan
	IndexBuilt               bool
	RejectedBitmapCandidates map[string]struct{}
	BitmapRenderBackOverride int
	TempSubtitleFile         string
	SubtitleFontDir          string
	DVDMediaInfoResult       DVDMediaInfoResult
	HasDVDMediaInfoResult    bool
}
