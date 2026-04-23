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
