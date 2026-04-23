package runtime

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
