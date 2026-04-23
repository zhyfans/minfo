package runtime

// MediaState 维护截图流程运行时的媒体时间轴和显示尺寸信息。
type MediaState struct {
	StartOffset   float64
	Duration      float64
	VideoWidth    int
	VideoHeight   int
	DisplayWidth  int
	DisplayHeight int
}
