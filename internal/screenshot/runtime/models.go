// Package runtime 定义截图运行时在子包之间共享的内部模型。
package runtime

// VariantSettings 描述单种截图输出格式对应的探测、搜索和编码参数。
type VariantSettings struct {
	Ext            string
	ProbeSize      string
	Analyze        string
	CoarseBackText int
	CoarseBackPGS  int
	RenderBackText int
	RenderBackPGS  int
	SearchBack     float64
	SearchForward  float64
	JPGQuality     int
}

// BitmapSubtitleKind 表示当前字幕流对应的位图字幕类型。
type BitmapSubtitleKind string

const (
	// BitmapSubtitleNone 表示当前字幕不是位图字幕。
	BitmapSubtitleNone BitmapSubtitleKind = ""
	// BitmapSubtitlePGS 表示当前字幕是 PGS 位图字幕。
	BitmapSubtitlePGS BitmapSubtitleKind = "pgs"
	// BitmapSubtitleDVD 表示当前字幕是 DVD 位图字幕。
	BitmapSubtitleDVD BitmapSubtitleKind = "dvd"
)

// SubtitleSelection 表示截图流程最终选中的字幕来源。
type SubtitleSelection struct {
	Mode          string
	File          string
	StreamIndex   int
	RelativeIndex int
	Lang          string
	Codec         string
	Title         string
	ExtractedText bool
}

// SubtitleSpan 表示一个字幕在时间轴上的可见区间。
type SubtitleSpan struct {
	Start float64
	End   float64
}

// SubtitleTrack 表示一次 ffprobe 探测得到的字幕流元数据。
type SubtitleTrack struct {
	Index     int
	StreamID  string
	Codec     string
	Language  string
	Title     string
	Forced    int
	IsDefault int
	Tags      string
}

// BlurayHelperTrack 表示 bdsub/helper 返回的蓝光字幕附加元数据。
type BlurayHelperTrack struct {
	PID          int    `json:"pid"`
	Lang         string `json:"lang"`
	CodingType   int    `json:"coding_type"`
	CharCode     int    `json:"char_code"`
	SubpathID    int    `json:"subpath_id"`
	PayloadBytes uint64 `json:"payload_bytes"`
	Bitrate      int64  `json:"bitrate"`
}

// BlurayHelperResult 表示 bdsub/helper 的一次完整输出。
type BlurayHelperResult struct {
	Source         string `json:"source"`
	BitrateScanned bool   `json:"bitrate_scanned"`
	BitrateMode    string `json:"bitrate_mode"`
	Clip           struct {
		ClipID        string              `json:"clip_id"`
		PGStreamCount int                 `json:"pg_stream_count"`
		PacketSeconds float64             `json:"packet_seconds"`
		PGStreams     []BlurayHelperTrack `json:"pg_streams"`
	} `json:"clip"`
}

// BlurayProbeContext 描述当前蓝光字幕探测所绑定的根目录、播放列表和 clip。
type BlurayProbeContext struct {
	Root     string
	Playlist string
	Clip     string
}

// PreferredSubtitleRank 表示候选字幕在自动选轨时的综合排序键。
type PreferredSubtitleRank struct {
	LangClass        string
	LangScore        int
	DispositionScore int
	PID              int
	PIDOK            bool
	BitmapKind       BitmapSubtitleKind
	PayloadBytes     uint64
	UsePayloadBytes  bool
	Bitrate          int64
	UseBitrate       bool
}

// FFprobeStreamsPayload 表示 ffprobe 的 streams JSON 载荷。
type FFprobeStreamsPayload struct {
	Streams []struct {
		Index       int                    `json:"index"`
		ID          interface{}            `json:"id"`
		CodecName   string                 `json:"codec_name"`
		Tags        map[string]interface{} `json:"tags"`
		Disposition struct {
			Default int `json:"default"`
			Forced  int `json:"forced"`
		} `json:"disposition"`
	} `json:"streams"`
}

// FFprobePacketsPayload 表示 ffprobe 的 packets JSON 载荷。
type FFprobePacketsPayload struct {
	Packets []FFprobePacket `json:"packets"`
}

// FFprobePacket 表示单个 ffprobe packet 的紧凑字段集合。
type FFprobePacket struct {
	PTSTime      string `json:"pts_time"`
	DurationTime string `json:"duration_time"`
	Size         string `json:"size"`
}
