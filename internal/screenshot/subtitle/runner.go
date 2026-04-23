package subtitle

import (
	"context"
	"fmt"
	"strings"

	screenshotdvdinfo "minfo/internal/screenshot/dvdinfo"
	screenshotruntime "minfo/internal/screenshot/runtime"
)

const (
	subtitleModeOff = "off"
	playlistScanMax = 6
)

// RunnerConfig 描述字幕执行流所需的运行期依赖和回调。
type RunnerConfig struct {
	Ctx                      context.Context
	SourcePath               string
	DVDMediaInfoPath         string
	SubtitleMode             string
	Settings                 screenshotruntime.VariantSettings
	Tools                    screenshotruntime.Toolchain
	Media                    *screenshotruntime.MediaState
	SubtitleState            *screenshotruntime.SubtitleState
	Subtitle                 *screenshotruntime.SubtitleSelection
	Logf                     func(format string, args ...interface{})
	LogProgress              func(stage string, current, total int, detail string)
	LogProgressPercent       func(stage string, percent float64, detail string)
	StartHeartbeat           func(stage, detail string) func()
	EnsureDVDMediaInfo       func() (screenshotruntime.DVDMediaInfoResult, bool, error)
	IsSupportedBitmap        func() bool
	RunFFmpegSubtitleExtract func(args []string) (string, string, error)
}

// Runner 承载字幕选轨、探测、预处理等执行流所需的最小上下文。
type Runner struct {
	Ctx                      context.Context
	SourcePath               string
	DVDMediaInfoPath         string
	SubtitleMode             string
	Settings                 screenshotruntime.VariantSettings
	Tools                    screenshotruntime.Toolchain
	Media                    *screenshotruntime.MediaState
	SubtitleState            *screenshotruntime.SubtitleState
	Subtitle                 *screenshotruntime.SubtitleSelection
	logfFunc                 func(format string, args ...interface{})
	logProgressFunc          func(stage string, current, total int, detail string)
	logProgressPercentFunc   func(stage string, percent float64, detail string)
	startHeartbeatFunc       func(stage, detail string) func()
	ensureDVDMediaInfoFunc   func() (screenshotruntime.DVDMediaInfoResult, bool, error)
	isSupportedBitmapFunc    func() bool
	runFFmpegSubtitleExtract func(args []string) (string, string, error)
}

// NewRunner 会基于截图运行器提供的配置创建一份字幕执行上下文。
func NewRunner(config RunnerConfig) *Runner {
	state := config.SubtitleState
	if state == nil {
		state = &screenshotruntime.SubtitleState{}
	}

	selection := config.Subtitle
	if selection == nil {
		selection = &screenshotruntime.SubtitleSelection{Mode: "none", RelativeIndex: -1, StreamIndex: -1}
	}

	return &Runner{
		Ctx:                      config.Ctx,
		SourcePath:               config.SourcePath,
		DVDMediaInfoPath:         config.DVDMediaInfoPath,
		SubtitleMode:             strings.TrimSpace(config.SubtitleMode),
		Settings:                 config.Settings,
		Tools:                    config.Tools,
		Media:                    config.Media,
		SubtitleState:            state,
		Subtitle:                 selection,
		logfFunc:                 config.Logf,
		logProgressFunc:          config.LogProgress,
		logProgressPercentFunc:   config.LogProgressPercent,
		startHeartbeatFunc:       config.StartHeartbeat,
		ensureDVDMediaInfoFunc:   config.EnsureDVDMediaInfo,
		isSupportedBitmapFunc:    config.IsSupportedBitmap,
		runFFmpegSubtitleExtract: config.RunFFmpegSubtitleExtract,
	}
}

func (r *Runner) state() *screenshotruntime.SubtitleState {
	if r == nil {
		return nil
	}
	if r.SubtitleState == nil {
		r.SubtitleState = &screenshotruntime.SubtitleState{}
	}
	return r.SubtitleState
}

func (r *Runner) media() *screenshotruntime.MediaState {
	if r == nil {
		return nil
	}
	if r.Media == nil {
		r.Media = &screenshotruntime.MediaState{}
	}
	return r.Media
}

func (r *Runner) selection() *screenshotruntime.SubtitleSelection {
	if r == nil {
		return nil
	}
	if r.Subtitle == nil {
		r.Subtitle = &screenshotruntime.SubtitleSelection{Mode: "none", RelativeIndex: -1, StreamIndex: -1}
	}
	return r.Subtitle
}

func (r *Runner) logf(format string, args ...interface{}) {
	if r == nil || r.logfFunc == nil {
		return
	}
	r.logfFunc(format, args...)
}

func (r *Runner) logProgress(stage string, current, total int, detail string) {
	if r == nil || r.logProgressFunc == nil {
		return
	}
	r.logProgressFunc(stage, current, total, detail)
}

func (r *Runner) logProgressPercent(stage string, percent float64, detail string) {
	if r == nil || r.logProgressPercentFunc == nil {
		return
	}
	r.logProgressPercentFunc(stage, percent, detail)
}

func (r *Runner) startHeartbeat(stage, detail string) func() {
	if r == nil || r.startHeartbeatFunc == nil {
		return func() {}
	}
	return r.startHeartbeatFunc(stage, detail)
}

func (r *Runner) ensureDVDMediaInfoResult() (screenshotruntime.DVDMediaInfoResult, bool, error) {
	if r == nil || r.ensureDVDMediaInfoFunc == nil {
		return screenshotruntime.DVDMediaInfoResult{}, false, nil
	}
	return r.ensureDVDMediaInfoFunc()
}

func (r *Runner) isSupportedBitmapSubtitle() bool {
	if r == nil || r.isSupportedBitmapFunc == nil {
		return false
	}
	return r.isSupportedBitmapFunc()
}

func (r *Runner) isPGSSubtitle() bool {
	return BitmapKindFromCodec(r.selection().Codec) == screenshotruntime.BitmapSubtitlePGS
}

func (r *Runner) isDVDSubtitle() bool {
	return BitmapKindFromCodec(r.selection().Codec) == screenshotruntime.BitmapSubtitleDVD
}

func (r *Runner) runSubtitleExtract(args []string) (string, string, error) {
	if r == nil || r.runFFmpegSubtitleExtract == nil {
		return "", "", fmt.Errorf("ffmpeg subtitle extraction not configured")
	}
	return r.runFFmpegSubtitleExtract(args)
}

func (r *Runner) subtitleProbeSource() string {
	if r == nil {
		return ""
	}
	return r.SourcePath
}

func (r *Runner) dvdProbeSource() string {
	if r == nil {
		return ""
	}
	return r.SourcePath
}

func (r *Runner) dvdMediaInfoSource() string {
	if r == nil {
		return ""
	}
	if strings.TrimSpace(r.DVDMediaInfoPath) != "" {
		return r.DVDMediaInfoPath
	}
	return r.dvdProbeSource()
}

func (r *Runner) dvdSelectedIFOPath() string {
	if resolved, ok := screenshotdvdinfo.IFOPath(r.dvdProbeSource()); ok {
		return resolved
	}
	if resolved, ok := screenshotdvdinfo.IFOPath(r.dvdMediaInfoSource()); ok {
		return resolved
	}
	return r.dvdMediaInfoSource()
}

func (r *Runner) dvdSelectedVOBPath() string {
	if resolved, ok := screenshotdvdinfo.TitleVOBPath(r.dvdProbeSource()); ok {
		return resolved
	}
	if resolved, ok := screenshotdvdinfo.TitleVOBPath(r.dvdMediaInfoSource()); ok {
		return resolved
	}
	return ""
}

func allDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, item := range value {
		if item < '0' || item > '9' {
			return false
		}
	}
	return true
}
