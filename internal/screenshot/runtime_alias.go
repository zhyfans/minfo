package screenshot

import screenshotruntime "minfo/internal/screenshot/runtime"

type variantSettings = screenshotruntime.VariantSettings
type bitmapSubtitleKind = screenshotruntime.BitmapSubtitleKind
type subtitleSelection = screenshotruntime.SubtitleSelection
type subtitleSpan = screenshotruntime.SubtitleSpan
type subtitleTrack = screenshotruntime.SubtitleTrack
type blurayHelperTrack = screenshotruntime.BlurayHelperTrack
type blurayHelperResult = screenshotruntime.BlurayHelperResult
type blurayProbeContext = screenshotruntime.BlurayProbeContext
type dvdMediaInfoTrack = screenshotruntime.DVDMediaInfoTrack
type dvdMediaInfoResult = screenshotruntime.DVDMediaInfoResult
type preferredSubtitleRank = screenshotruntime.PreferredSubtitleRank
type ffprobeStreamsPayload = screenshotruntime.FFprobeStreamsPayload
type ffprobePacketsPayload = screenshotruntime.FFprobePacketsPayload
type ffprobePacket = screenshotruntime.FFprobePacket
type activeShotState = screenshotruntime.ActiveShot
type runtimeLogger = screenshotruntime.Logger
type runtimeMediaState = screenshotruntime.MediaState
type runtimeRenderState = screenshotruntime.RenderState
type runtimeSubtitleState = screenshotruntime.SubtitleState
type runtimeToolchain = screenshotruntime.Toolchain

const (
	bitmapSubtitleNone      = screenshotruntime.BitmapSubtitleNone
	bitmapSubtitlePGS       = screenshotruntime.BitmapSubtitlePGS
	bitmapSubtitleDVD       = screenshotruntime.BitmapSubtitleDVD
	activeShotPhaseRender   = screenshotruntime.ActiveShotPhaseRender
	activeShotPhaseReencode = screenshotruntime.ActiveShotPhaseReencode
)

// variantSettingsFor 会通过 runtime 子包返回当前输出格式对应的参数集合。
func variantSettingsFor(variant string) variantSettings {
	return screenshotruntime.VariantSettingsFor(variant)
}

// newRuntimeLogger 会基于外部实时日志回调创建运行期日志器。
func newRuntimeLogger(handler LogHandler) runtimeLogger {
	return screenshotruntime.NewLogger(handler)
}
