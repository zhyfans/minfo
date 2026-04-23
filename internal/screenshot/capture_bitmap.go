// Package screenshot 提供位图字幕探测与截图渲染逻辑。

package screenshot

import (
	"fmt"
	"strings"

	screenshottimestamps "minfo/internal/screenshot/timestamps"
	"minfo/internal/system"
)

// bitmapSubtitleVisibleAt 判断当前内部位图字幕在给定时间点是否真的可见。
func (r *screenshotRunner) bitmapSubtitleVisibleAt(aligned float64) (bool, error) {
	if !r.isSupportedBitmapSubtitle() || r.subtitle.Mode != "internal" {
		return false, nil
	}

	switch {
	case r.isPGSSubtitle():
		return r.pgsSubtitleVisibleAt(aligned)
	case r.isDVDSubtitle():
		return r.dvdSubtitleVisibleAt(aligned)
	default:
		return false, nil
	}
}

// pgsSubtitleVisibleAt 复用通用的内部位图可见性检测逻辑判断 PGS 字幕是否可见。
func (r *screenshotRunner) pgsSubtitleVisibleAt(aligned float64) (bool, error) {
	return r.internalBitmapSubtitleVisibleAt(aligned)
}

// dvdSubtitleVisibleAt 复用通用的内部位图可见性检测逻辑判断 DVD 字幕是否可见。
func (r *screenshotRunner) dvdSubtitleVisibleAt(aligned float64) (bool, error) {
	return r.internalBitmapSubtitleVisibleAt(aligned)
}

// internalBitmapSubtitleVisibleAt 通过比较有无字幕叠加的探测帧来判断位图字幕是否显示出来。
func (r *screenshotRunner) internalBitmapSubtitleVisibleAt(aligned float64) (bool, error) {
	return r.internalBitmapSubtitleVisibleAtWithCoarseBack(aligned, r.renderCoarseBack())
}

// internalBitmapSubtitleVisibleAtWithCoarseBack 会使用指定回溯窗口检查位图字幕是否真正显示。
func (r *screenshotRunner) internalBitmapSubtitleVisibleAtWithCoarseBack(aligned float64, coarseBack int) (bool, error) {
	baseFrame, err := r.captureBitmapProbeFrame(r.sourcePath, aligned, false, coarseBack)
	if err != nil {
		return false, err
	}
	subFrame, err := r.captureBitmapProbeFrame(r.sourcePath, aligned, true, coarseBack)
	if err != nil {
		return false, err
	}
	return baseFrame != subFrame, nil
}

// captureBitmapProbeFrame 抓取一帧灰度探测图，用于判断位图字幕在该时刻是否可见。
func (r *screenshotRunner) captureBitmapProbeFrame(inputPath string, localTime float64, withSubtitle bool, coarseBack int) (string, error) {
	if coarseBack <= 0 {
		coarseBack = r.settings.CoarseBackPGS
	}
	_, fineSecond, coarseHMS := r.splitCaptureTimeline(localTime, coarseBack)

	args := []string{
		"-v", "error",
		"-fflags", "+genpts",
		"-ss", coarseHMS,
		"-probesize", r.settings.ProbeSize,
		"-analyzeduration", r.settings.Analyze,
		"-i", inputPath,
		"-ss", screenshottimestamps.FormatFloat(fineSecond),
		"-frames:v", "1",
		"-f", "rawvideo",
		"-pix_fmt", "gray",
	}

	if withSubtitle {
		args = append(args, r.bitmapProbeOutputArgs()...)
	} else {
		filterChain := joinFilters(r.displayAspectFilter(), "format=gray")
		args = append(args,
			"-map", "0:v:0",
			"-vf", filterChain,
			"-",
		)
	}

	stdout, stderr, err := system.RunCommand(r.ctx, r.tools.FFmpegBin, args...)
	if err != nil {
		return "", fmt.Errorf(system.BestErrorMessage(err, stderr, stdout))
	}
	return stdout, nil
}

// bitmapProbeOutputArgs 返回位图字幕探测帧在启用字幕叠加时的输出参数。
func (r *screenshotRunner) bitmapProbeOutputArgs() []string {
	filterComplex := r.buildInternalBitmapProbeFilterComplex()
	if r.isPGSSubtitle() {
		filterComplex = r.buildPGSOverlayFilterComplex(r.displayAspectFilter(), "format=gray")
	}
	return []string{
		"-filter_complex", filterComplex,
		"-map", "[out]",
		"-",
	}
}

// buildInternalBitmapProbeFilterComplex 会构造 DVD 等内封位图字幕探测时使用的 filter_complex。
func (r *screenshotRunner) buildInternalBitmapProbeFilterComplex() string {
	return fmt.Sprintf("[0:v:0][0:s:%d]overlay=(W-w)/2:(H-h-10),%s,format=gray[out]",
		r.subtitle.RelativeIndex,
		r.displayAspectFilter(),
	)
}

// capturePGSPrimary 复用内部位图字幕主流程渲染 PGS 截图。
func (r *screenshotRunner) capturePGSPrimary(coarseHMS string, fineSecond float64, path string) error {
	return r.capturePGSBitmapWithOutputArgs(coarseHMS, fineSecond, r.primaryOutputArgs(), path)
}

// captureDVDPrimary 复用内部位图字幕主流程渲染 DVD 截图。
func (r *screenshotRunner) captureDVDPrimary(coarseHMS string, fineSecond float64, path string) error {
	return r.captureInternalBitmapPrimary(coarseHMS, fineSecond, path)
}

// captureInternalBitmapPrimary 使用 overlay 叠加字幕轨，渲染内封位图字幕的主流程截图。
func (r *screenshotRunner) captureInternalBitmapPrimary(coarseHMS string, fineSecond float64, path string) error {
	args := r.internalBitmapCaptureArgs(coarseHMS, fineSecond, r.primaryOutputArgs(), path)
	return r.runFFmpeg(args, fineSecond)
}

// capturePGSBitmapWithOutputArgs 会在视频处理完成后再叠加 PGS 字幕并输出截图。
func (r *screenshotRunner) capturePGSBitmapWithOutputArgs(coarseHMS string, fineSecond float64, outputArgs []string, path string) error {
	args := []string{
		"-v", "error",
		"-fflags", "+genpts",
		"-ss", coarseHMS,
		"-probesize", r.settings.ProbeSize,
		"-analyzeduration", r.settings.Analyze,
		"-i", r.sourcePath,
		"-ss", screenshottimestamps.FormatFloat(fineSecond),
		"-filter_complex", r.buildPGSRenderFilterComplex(),
		"-map", "[out]",
		"-frames:v", "1",
		"-y",
	}
	args = append(args, outputArgs...)
	args = append(args, path)
	return r.runFFmpeg(args, fineSecond)
}

// captureTextSubtitleWithOutputArgs 会让文字字幕渲染和视频取帧共用同一条时间轴，避免字幕与画面错位。
func (r *screenshotRunner) captureTextSubtitleWithOutputArgs(aligned float64, outputArgs []string, path string) error {
	subFilter := r.buildTextSubtitleFilter()
	if strings.TrimSpace(subFilter) == "" {
		return fmt.Errorf("text subtitle filter is empty")
	}

	coarseSecond, _, coarseHMS := r.splitCaptureTimeline(aligned, r.renderCoarseBack())
	filterChain := r.buildTextSubtitleRenderChain(float64(coarseSecond), aligned, subFilter)

	args := []string{
		"-v", "error",
		"-fflags", "+genpts",
		"-ss", coarseHMS,
		"-probesize", r.settings.ProbeSize,
		"-analyzeduration", r.settings.Analyze,
		"-i", r.sourcePath,
		"-map", "0:v:0",
		"-y",
		"-frames:v", "1",
		"-vf", filterChain,
	}
	args = append(args, outputArgs...)
	args = append(args, path)
	return r.runFFmpeg(args, aligned-float64(coarseSecond))
}

// capturePGSJPGReencoded 复用内部位图 JPG 重拍流程处理 PGS 截图。
func (r *screenshotRunner) capturePGSJPGReencoded(coarseHMS string, fineSecond float64, quality int, path string) error {
	return r.capturePGSBitmapWithOutputArgs(coarseHMS, fineSecond, jpgReencodeOutputArgs(quality), path)
}

// captureDVDJPGReencoded 复用内部位图 JPG 重拍流程处理 DVD 截图。
func (r *screenshotRunner) captureDVDJPGReencoded(coarseHMS string, fineSecond float64, quality int, path string) error {
	return r.captureInternalBitmapJPGReencoded(coarseHMS, fineSecond, quality, path)
}

// captureInternalBitmapPNGReencoded 用 PNG 重新渲染带内封位图字幕的截图。
func (r *screenshotRunner) captureInternalBitmapPNGReencoded(coarseHMS string, fineSecond float64, path string) error {
	args := r.internalBitmapCaptureArgs(coarseHMS, fineSecond, pngReencodeOutputArgs(), path)
	return r.runFFmpeg(args, fineSecond)
}

// captureInternalBitmapJPGReencoded 用 JPG 重新渲染带内封位图字幕的截图。
func (r *screenshotRunner) captureInternalBitmapJPGReencoded(coarseHMS string, fineSecond float64, quality int, path string) error {
	args := r.internalBitmapCaptureArgs(coarseHMS, fineSecond, jpgReencodeOutputArgs(quality), path)
	return r.runFFmpeg(args, fineSecond)
}

// internalBitmapCaptureArgs 会构造 DVD 等内封位图字幕截图使用的 ffmpeg 参数。
func (r *screenshotRunner) internalBitmapCaptureArgs(coarseHMS string, fineSecond float64, outputArgs []string, path string) []string {
	args := []string{
		"-v", "error",
		"-fflags", "+genpts",
		"-ss", coarseHMS,
		"-probesize", r.settings.ProbeSize,
		"-analyzeduration", r.settings.Analyze,
		"-i", r.sourcePath,
		"-ss", screenshottimestamps.FormatFloat(fineSecond),
		"-filter_complex", r.buildInternalBitmapRenderFilterComplex(),
		"-frames:v", "1",
		"-y",
	}
	args = append(args, outputArgs...)
	args = append(args, path)
	return args
}

// buildInternalBitmapRenderFilterComplex 会构造内封位图字幕主截图使用的 filter_complex。
func (r *screenshotRunner) buildInternalBitmapRenderFilterComplex() string {
	return joinFilters(
		fmt.Sprintf("[0:v:0][0:s:%d]overlay=(W-w)/2:(H-h-10)", r.subtitle.RelativeIndex),
		r.render.ColorChain,
		r.displayAspectFilter(),
	)
}
