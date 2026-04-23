// Package screenshot 实现截图渲染和位图字幕绘制流程。

package screenshot

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"minfo/internal/system"
)

// captureScreenshot 会执行一次完整截图，并在文件过大时自动触发重编码兜底。
func (r *screenshotRunner) captureScreenshot(aligned float64, path string) error {
	r.activeShot.SetPhase(activeShotPhaseRender)
	if err := r.runRenderWithLibplaceboFallback(func() error {
		return r.capturePrimary(aligned, path)
	}); err != nil {
		return err
	}

	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.Size() <= oversizeBytes {
		return nil
	}

	sizeMB := float64(info.Size()) / 1024.0 / 1024.0
	if r.variant != VariantJPG {
		r.logf("[提示] %s 大小 %.2fMB，先使用 oxipng 压缩...", filepath.Base(path), sizeMB)
		r.compressOversizedPNGIfNeeded(path)
		return nil
	}

	r.logf("[提示] %s 大小 %.2fMB，重拍降低质量...", filepath.Base(path), sizeMB)
	tempPath := path + ".tmp" + r.settings.Ext
	r.activeShot.SetPhase(activeShotPhaseReencode)
	if err := r.runRenderWithLibplaceboFallback(func() error {
		return r.captureReencoded(aligned, tempPath)
	}); err != nil {
		_ = os.Remove(tempPath)
		r.logf("[警告] 重拍失败，保留原始截图：%s", err.Error())
		r.activeShot.SetPhase(activeShotPhaseRender)
		return nil
	}
	if err := os.Rename(tempPath, path); err != nil {
		_ = os.Remove(tempPath)
		return err
	}
	r.activeShot.SetPhase(activeShotPhaseRender)
	return nil
}

// compressOversizedPNGIfNeeded 会在 PNG 截图过大时依次尝试无损和有损压缩。
func (r *screenshotRunner) compressOversizedPNGIfNeeded(path string) {
	info, err := os.Stat(path)
	if err != nil || info.Size() <= oversizeBytes {
		return
	}

	if err := r.compressOxiPNG(path); err != nil {
		r.logf("[警告] %s oxipng 压缩失败，保留当前截图：%s", filepath.Base(path), err.Error())
		return
	}

	afterInfo, err := os.Stat(path)
	if err != nil {
		return
	}
	afterMB := float64(afterInfo.Size()) / 1024.0 / 1024.0
	if afterInfo.Size() <= oversizeBytes {
		r.logf("[信息] %s 经 oxipng 压缩后大小 %.2fMB。", filepath.Base(path), afterMB)
		return
	}

	r.logf("[提示] %s 经 oxipng 压缩后仍为 %.2fMB，继续使用 pngquant 压缩...", filepath.Base(path), afterMB)
	if err := r.compressPNGQuant(path); err != nil {
		r.logf("[警告] %s pngquant 压缩失败，保留 oxipng 结果：%s", filepath.Base(path), err.Error())
		return
	}
	r.logf("[警告] %s 已使用 pngquant 有损压缩。若介意画质损失，可切换 JPG 重新生成。", filepath.Base(path))

	finalInfo, err := os.Stat(path)
	if err != nil {
		return
	}

	finalMB := float64(finalInfo.Size()) / 1024.0 / 1024.0
	if finalInfo.Size() <= oversizeBytes {
		r.logf("[信息] %s 经 oxipng + pngquant 压缩后大小 %.2fMB。", filepath.Base(path), finalMB)
		return
	}

	r.logf("[警告] %s 经 oxipng + pngquant 压缩后仍为 %.2fMB，图床上传可能跳过该文件。", filepath.Base(path), finalMB)
}

// compressOxiPNG 会调用 oxipng 对 PNG 截图执行无损压缩。
func (r *screenshotRunner) compressOxiPNG(path string) error {
	if strings.TrimSpace(r.tools.OxiPNGBin) == "" {
		return fmt.Errorf("%s not found", system.OxiPNGBinaryPath)
	}

	args := buildOxiPNGCompressionArgs(path)
	stdout, stderr, err := system.RunCommand(r.ctx, r.tools.OxiPNGBin, args...)
	if err != nil {
		return fmt.Errorf(system.BestErrorMessage(err, stderr, stdout))
	}
	return nil
}

// compressPNGQuant 会调用 pngquant 生成替换式的有损压缩 PNG。
func (r *screenshotRunner) compressPNGQuant(path string) error {
	if strings.TrimSpace(r.tools.PNGQuantBin) == "" {
		return fmt.Errorf("%s not found", system.PNGQuantBinaryPath)
	}

	compressedPath := path + ".quant.png"
	_ = os.Remove(compressedPath)

	args := buildPNGQuantCompressionArgs(path, compressedPath)
	stdout, stderr, err := system.RunCommand(r.ctx, r.tools.PNGQuantBin, args...)
	if err != nil {
		_ = os.Remove(compressedPath)
		return fmt.Errorf(system.BestErrorMessage(err, stderr, stdout))
	}

	if err := os.Rename(compressedPath, path); err != nil {
		_ = os.Remove(compressedPath)
		return err
	}
	r.markLossyPNG(path)
	return nil
}

// buildOxiPNGCompressionArgs 会构造 oxipng 压缩当前截图所需的参数列表。
func buildOxiPNGCompressionArgs(path string) []string {
	return []string{
		"-o", "max",
		"--strip", "safe",
		"--quiet",
		path,
	}
}

// buildPNGQuantCompressionArgs 会构造 pngquant 输出到目标文件的参数列表。
func buildPNGQuantCompressionArgs(inputPath, outputPath string) []string {
	return []string{
		"256",
		"--force",
		"--output", outputPath,
		"--speed", "1",
		"--strip",
		"--",
		inputPath,
	}
}

// markLossyPNG 会记录当前截图已经经过有损 PNG 压缩。
func (r *screenshotRunner) markLossyPNG(path string) {
	if r == nil {
		return
	}
	if r.lossyPNGFiles == nil {
		r.lossyPNGFiles = make(map[string]struct{})
	}
	name := filepath.Base(strings.TrimSpace(path))
	if name == "" {
		return
	}
	r.lossyPNGFiles[name] = struct{}{}
}

// lossyPNGFileList 会返回按文件名排序的有损 PNG 清单。
func (r *screenshotRunner) lossyPNGFileList() []string {
	if r == nil || len(r.lossyPNGFiles) == 0 {
		return nil
	}
	files := make([]string, 0, len(r.lossyPNGFiles))
	for name := range r.lossyPNGFiles {
		files = append(files, name)
	}
	sort.Strings(files)
	return files
}

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
	coarseSecond := int(math.Max(math.Floor(localTime)-float64(coarseBack), 0))
	fineSecond := localTime - float64(coarseSecond)
	coarseHMS := formatTimestamp(coarseSecond)

	args := []string{
		"-v", "error",
		"-fflags", "+genpts",
		"-ss", coarseHMS,
		"-probesize", r.settings.ProbeSize,
		"-analyzeduration", r.settings.Analyze,
		"-i", inputPath,
		"-ss", formatFloat(fineSecond),
		"-frames:v", "1",
		"-f", "rawvideo",
		"-pix_fmt", "gray",
	}

	if withSubtitle {
		filterComplex := fmt.Sprintf("[0:v:0][0:s:%d]overlay=(W-w)/2:(H-h-10),%s,format=gray[out]",
			r.subtitle.RelativeIndex,
			r.displayAspectFilter(),
		)
		if r.isPGSSubtitle() {
			filterComplex = r.buildPGSOverlayFilterComplex(r.displayAspectFilter(), "format=gray")
		}
		args = append(args,
			"-filter_complex", filterComplex,
			"-map", "[out]",
			"-",
		)
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
	filterComplex := joinFilters(
		fmt.Sprintf("[0:v:0][0:s:%d]overlay=(W-w)/2:(H-h-10)", r.subtitle.RelativeIndex),
		r.render.ColorChain,
		r.displayAspectFilter(),
	)
	args := []string{
		"-v", "error",
		"-fflags", "+genpts",
		"-ss", coarseHMS,
		"-probesize", r.settings.ProbeSize,
		"-analyzeduration", r.settings.Analyze,
		"-i", r.sourcePath,
		"-ss", formatFloat(fineSecond),
		"-filter_complex", filterComplex,
		"-frames:v", "1",
		"-y",
	}
	args = append(args, r.primaryOutputArgs()...)
	args = append(args, path)
	return r.runFFmpeg(args, fineSecond)
}

// capturePGSBitmapWithOutputArgs 会在视频处理完成后再叠加 PGS 字幕并输出截图。
func (r *screenshotRunner) capturePGSBitmapWithOutputArgs(coarseHMS string, fineSecond float64, outputArgs []string, path string) error {
	filterComplex := r.buildPGSRenderFilterComplex()
	args := []string{
		"-v", "error",
		"-fflags", "+genpts",
		"-ss", coarseHMS,
		"-probesize", r.settings.ProbeSize,
		"-analyzeduration", r.settings.Analyze,
		"-i", r.sourcePath,
		"-ss", formatFloat(fineSecond),
		"-filter_complex", filterComplex,
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

	coarseBack := r.renderCoarseBack()
	coarseSecond := int(math.Max(math.Floor(aligned)-float64(coarseBack), 0))
	coarseHMS := formatTimestamp(coarseSecond)
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

// capturePrimary 执行首选截图路径，并根据字幕类型选择对应的渲染方案。
func (r *screenshotRunner) capturePrimary(aligned float64, path string) error {
	if r.subtitle.Mode == "external" {
		if _, err := os.Stat(r.subtitle.File); err != nil {
			return fmt.Errorf("subtitle file not found before render: %w", err)
		}
	}

	coarseBack := r.renderCoarseBack()

	coarseSecond := int(math.Max(math.Floor(aligned)-float64(coarseBack), 0))
	fineSecond := aligned - float64(coarseSecond)
	coarseHMS := formatTimestamp(coarseSecond)

	if r.subtitle.Mode == "internal" {
		switch {
		case r.isPGSSubtitle():
			return r.capturePGSPrimary(coarseHMS, fineSecond, path)
		case r.isDVDSubtitle():
			return r.captureDVDPrimary(coarseHMS, fineSecond, path)
		}
	}

	filterChain := joinFilters(r.render.ColorChain, r.displayAspectFilter())

	if subFilter := r.buildTextSubtitleFilter(); subFilter != "" {
		return r.captureTextSubtitleWithOutputArgs(aligned, r.primaryOutputArgs(), path)
	}

	args := []string{
		"-v", "error",
		"-fflags", "+genpts",
		"-ss", coarseHMS,
		"-probesize", r.settings.ProbeSize,
		"-analyzeduration", r.settings.Analyze,
		"-i", r.sourcePath,
		"-ss", formatFloat(fineSecond),
		"-map", "0:v:0",
		"-y",
		"-frames:v", "1",
		"-vf", filterChain,
	}
	args = append(args, r.primaryOutputArgs()...)
	args = append(args, path)
	return r.runFFmpeg(args, fineSecond)
}

// captureReencoded 在原始截图过大时用更保守的编码参数重新截图。
func (r *screenshotRunner) captureReencoded(aligned float64, path string) error {
	if r.variant == VariantJPG {
		return r.captureJPGReencoded(aligned, path)
	}
	return r.capturePNGReencoded(aligned, path)
}

// capturePNGReencoded 用 PNG 重拍截图，并在需要时加入色彩空间转换链。
func (r *screenshotRunner) capturePNGReencoded(aligned float64, path string) error {
	coarseBack := r.renderCoarseBack()

	coarseSecond := int(math.Max(math.Floor(aligned)-float64(coarseBack), 0))
	fineSecond := aligned - float64(coarseSecond)
	coarseHMS := formatTimestamp(coarseSecond)

	if r.subtitle.Mode == "internal" {
		switch {
		case r.isPGSSubtitle():
			return r.capturePGSPNGReencoded(coarseHMS, fineSecond, path)
		case r.isDVDSubtitle():
			return r.captureDVDPNGReencoded(coarseHMS, fineSecond, path)
		}
	}

	filterChain := joinFilters(r.render.ColorChain, r.displayAspectFilter())
	if subFilter := r.buildTextSubtitleFilter(); subFilter != "" {
		return r.captureTextSubtitleWithOutputArgs(aligned, []string{
			"-c:v", "png",
			"-compression_level", "9",
			"-pred", "mixed",
		}, path)
	}

	args := []string{
		"-v", "error",
		"-fflags", "+genpts",
		"-ss", coarseHMS,
		"-probesize", r.settings.ProbeSize,
		"-analyzeduration", r.settings.Analyze,
		"-i", r.sourcePath,
		"-ss", formatFloat(fineSecond),
		"-map", "0:v:0",
		"-frames:v", "1",
		"-y",
		"-vf", filterChain,
		"-c:v", "png",
		"-compression_level", "9",
		"-pred", "mixed",
		path,
	}
	return r.runFFmpeg(args, fineSecond)
}

// capturePGSPNGReencoded 复用内部位图 PNG 重拍流程处理 PGS 截图。
func (r *screenshotRunner) capturePGSPNGReencoded(coarseHMS string, fineSecond float64, path string) error {
	return r.capturePGSBitmapWithOutputArgs(coarseHMS, fineSecond, []string{
		"-c:v", "png",
		"-compression_level", "9",
		"-pred", "mixed",
	}, path)
}

// captureDVDPNGReencoded 复用内部位图 PNG 重拍流程处理 DVD 截图。
func (r *screenshotRunner) captureDVDPNGReencoded(coarseHMS string, fineSecond float64, path string) error {
	return r.captureInternalBitmapPNGReencoded(coarseHMS, fineSecond, path)
}

// captureInternalBitmapPNGReencoded 用 PNG 重新渲染带内封位图字幕的截图。
func (r *screenshotRunner) captureInternalBitmapPNGReencoded(coarseHMS string, fineSecond float64, path string) error {
	filterComplex := joinFilters(
		fmt.Sprintf("[0:v:0][0:s:%d]overlay=(W-w)/2:(H-h-10)", r.subtitle.RelativeIndex),
		r.render.ColorChain,
		r.displayAspectFilter(),
	)
	args := []string{
		"-v", "error",
		"-fflags", "+genpts",
		"-ss", coarseHMS,
		"-probesize", r.settings.ProbeSize,
		"-analyzeduration", r.settings.Analyze,
		"-i", r.sourcePath,
		"-ss", formatFloat(fineSecond),
		"-filter_complex", filterComplex,
		"-frames:v", "1",
		"-y",
		"-c:v", "png",
		"-compression_level", "9",
		"-pred", "mixed",
		path,
	}
	return r.runFFmpeg(args, fineSecond)
}

// captureJPGReencoded 用更低质量的 JPG 参数重新截图以控制文件体积。
func (r *screenshotRunner) captureJPGReencoded(aligned float64, path string) error {
	coarseBack := r.renderCoarseBack()

	coarseSecond := int(math.Max(math.Floor(aligned)-float64(coarseBack), 0))
	fineSecond := aligned - float64(coarseSecond)
	coarseHMS := formatTimestamp(coarseSecond)

	quality := fallbackJPGQScale(r.settings.JPGQuality)

	if r.subtitle.Mode == "internal" {
		switch {
		case r.isPGSSubtitle():
			return r.capturePGSJPGReencoded(coarseHMS, fineSecond, quality, path)
		case r.isDVDSubtitle():
			return r.captureDVDJPGReencoded(coarseHMS, fineSecond, quality, path)
		}
	}

	filterChain := joinFilters(r.render.ColorChain, r.displayAspectFilter())
	if subFilter := r.buildTextSubtitleFilter(); subFilter != "" {
		return r.captureTextSubtitleWithOutputArgs(aligned, []string{
			"-c:v", "mjpeg",
			"-q:v", strconv.Itoa(quality),
		}, path)
	}

	args := []string{
		"-v", "error",
		"-fflags", "+genpts",
		"-ss", coarseHMS,
		"-probesize", r.settings.ProbeSize,
		"-analyzeduration", r.settings.Analyze,
		"-i", r.sourcePath,
		"-ss", formatFloat(fineSecond),
		"-map", "0:v:0",
		"-frames:v", "1",
		"-y",
		"-vf", filterChain,
		"-c:v", "mjpeg",
		"-q:v", strconv.Itoa(quality),
		path,
	}
	return r.runFFmpeg(args, fineSecond)
}

// capturePGSJPGReencoded 复用内部位图 JPG 重拍流程处理 PGS 截图。
func (r *screenshotRunner) capturePGSJPGReencoded(coarseHMS string, fineSecond float64, quality int, path string) error {
	return r.capturePGSBitmapWithOutputArgs(coarseHMS, fineSecond, []string{
		"-c:v", "mjpeg",
		"-q:v", strconv.Itoa(quality),
	}, path)
}

// captureDVDJPGReencoded 复用内部位图 JPG 重拍流程处理 DVD 截图。
func (r *screenshotRunner) captureDVDJPGReencoded(coarseHMS string, fineSecond float64, quality int, path string) error {
	return r.captureInternalBitmapJPGReencoded(coarseHMS, fineSecond, quality, path)
}

// captureInternalBitmapJPGReencoded 用 JPG 重新渲染带内封位图字幕的截图。
func (r *screenshotRunner) captureInternalBitmapJPGReencoded(coarseHMS string, fineSecond float64, quality int, path string) error {
	filterComplex := joinFilters(
		fmt.Sprintf("[0:v:0][0:s:%d]overlay=(W-w)/2:(H-h-10)", r.subtitle.RelativeIndex),
		r.render.ColorChain,
		r.displayAspectFilter(),
	)
	args := []string{
		"-v", "error",
		"-fflags", "+genpts",
		"-ss", coarseHMS,
		"-probesize", r.settings.ProbeSize,
		"-analyzeduration", r.settings.Analyze,
		"-i", r.sourcePath,
		"-ss", formatFloat(fineSecond),
		"-filter_complex", filterComplex,
		"-frames:v", "1",
		"-y",
		"-c:v", "mjpeg",
		"-q:v", strconv.Itoa(quality),
		path,
	}
	return r.runFFmpeg(args, fineSecond)
}

// primaryOutputArgs 返回主流程截图所需的输出编码参数。
func (r *screenshotRunner) primaryOutputArgs() []string {
	if r.variant == VariantJPG {
		return []string{"-c:v", "mjpeg", "-q:v", strconv.Itoa(clampJPGQScale(r.settings.JPGQuality))}
	}
	return []string{"-c:v", "png", "-compression_level", "9", "-pred", "mixed"}
}

// runFFmpeg 会执行FFmpeg，并把结果和错误状态返回给调用方。
func (r *screenshotRunner) runFFmpeg(args []string, localWindowSeconds float64) error {
	_, _, err := r.runFFmpegLive(args, "渲染", normalizeRenderProgressWindow(localWindowSeconds), r.ffmpegRenderProgressDetail)
	return err
}

// runRenderWithLibplaceboFallback 会在 libplacebo 渲染崩溃时自动切回兼容链后重试。
func (r *screenshotRunner) runRenderWithLibplaceboFallback(render func() error) error {
	if render == nil {
		return nil
	}

	err := render()
	if err == nil {
		return nil
	}
	if !r.applyLibplaceboRenderFallback(err) {
		return err
	}
	return render()
}

// runFFmpegSubtitleExtract 会执行带实时进度的字幕提取 FFmpeg 命令。
func (r *screenshotRunner) runFFmpegSubtitleExtract(args []string) (string, string, error) {
	return r.runFFmpegLive(args, "字幕", 0, r.ffmpegSubtitleProgressDetail)
}

// runFFmpegLive 会执行带实时进度回调的 FFmpeg 命令，并收集完整输出。
func (r *screenshotRunner) runFFmpegLive(args []string, stage string, localWindowSeconds float64, detailBuilder func(*ffmpegRealtimeState) string) (string, string, error) {
	extraArgs := 4
	if stage == "渲染" && r.usesLibplaceboColorspace() {
		extraArgs += 4
	}
	ffmpegArgs := make([]string, 0, len(args)+extraArgs)
	ffmpegArgs = append(ffmpegArgs, "-progress", "pipe:1", "-nostats")
	if stage == "渲染" && r.usesLibplaceboColorspace() {
		ffmpegArgs = append(ffmpegArgs,
			"-init_hw_device", "vulkan=minfo:llvmpipe",
			"-filter_hw_device", "minfo",
		)
	}
	ffmpegArgs = append(ffmpegArgs, args...)
	r.logf("[ffmpeg][%s] 执行命令: %s", stage, system.FormatCommandForLog(r.ctx, r.tools.FFmpegBin, ffmpegArgs...))

	progress := ffmpegRealtimeState{
		startedAt:     time.Now(),
		windowSeconds: localWindowSeconds,
	}
	done := make(chan struct{})
	defer close(done)

	if stage == "渲染" || stage == "字幕" {
		go func() {
			ticker := time.NewTicker(250 * time.Millisecond)
			defer ticker.Stop()

			for {
				select {
				case <-r.ctx.Done():
					return
				case <-done:
					return
				case <-ticker.C:
					r.emitFFmpegRealtimeProgress("continue", &progress, stage, detailBuilder)
				}
			}
		}()
	}

	stdout, stderr, err := system.RunCommandLive(r.ctx, r.tools.FFmpegBin, func(stream, line string) {
		if stream != "stdout" {
			return
		}
		r.consumeFFmpegProgressLine(strings.TrimSpace(line), &progress, stage, detailBuilder)
	}, ffmpegArgs...)
	if err != nil {
		message := strings.TrimSpace(stderr)
		if message == "" {
			message = err.Error()
		}
		return stdout, stderr, fmt.Errorf("%s", message)
	}
	return stdout, stderr, nil
}

// usesLibplaceboColorspace 会判断当前渲染链是否正在使用 libplacebo。
func (r *screenshotRunner) usesLibplaceboColorspace() bool {
	return r != nil && strings.Contains(r.render.ColorChain, "libplacebo=")
}

// applyLibplaceboRenderFallback 会在识别到崩溃特征时切换到兼容色彩链。
func (r *screenshotRunner) applyLibplaceboRenderFallback(err error) bool {
	if r == nil || err == nil || !r.usesLibplaceboColorspace() {
		return false
	}
	if !isLibplaceboRenderCrashMessage(err.Error()) {
		return false
	}

	fallbackChain := buildColorspaceChain(r.render.ColorInfo, false)
	if strings.TrimSpace(fallbackChain) == "" {
		return false
	}

	r.tools.LibplaceboReady = false
	r.render.ColorChain = fallbackChain
	r.logf("[提示] libplacebo/Vulkan 渲染失败，自动回退到兼容色彩链后重试当前截图。")
	return true
}

// isLibplaceboRenderCrashMessage 会判断错误文本是否命中已知的 libplacebo 崩溃特征。
func isLibplaceboRenderCrashMessage(message string) bool {
	lower := strings.ToLower(strings.TrimSpace(message))
	if lower == "" {
		return false
	}
	if strings.Contains(lower, "llvm error:") && strings.Contains(lower, "cannot select:") {
		return true
	}
	if strings.Contains(lower, "in function: cs_co_variant.resume") {
		return true
	}
	if strings.Contains(lower, "assertion failed:") && strings.Contains(lower, "pl_alloc.c") {
		return true
	}
	if strings.Contains(lower, "signal: segmentation fault") {
		return true
	}
	if strings.Contains(lower, "segmentation fault (core dumped)") {
		return true
	}
	return false
}

// renderCoarseBack 会根据字幕类型返回渲染阶段使用的回溯秒数。
func (r *screenshotRunner) renderCoarseBack() int {
	if r == nil {
		return 1
	}
	if r.subtitle.Mode == "internal" && r.isSupportedBitmapSubtitle() {
		if r.subtitleState.BitmapRenderBackOverride > 0 {
			return r.subtitleState.BitmapRenderBackOverride
		}
		if r.settings.RenderBackPGS > 0 {
			return r.settings.RenderBackPGS
		}
		return r.settings.CoarseBackPGS
	}
	if r.settings.RenderBackText > 0 {
		return r.settings.RenderBackText
	}
	return r.settings.CoarseBackText
}

type ffmpegRealtimeState struct {
	mu                sync.Mutex
	frame             string
	fps               string
	outTime           string
	outTimeMS         int64
	speed             string
	totalSize         string
	heartbeatCount    int
	lastLoggedPercent float64
	lastLoggedDetail  string
	startedAt         time.Time
	windowSeconds     float64
	firstOutTimeMS    int64
	hasFirstOutTime   bool
}

// consumeFFmpegProgressLine 会把单行 -progress 输出更新到实时进度状态。
func (r *screenshotRunner) consumeFFmpegProgressLine(line string, state *ffmpegRealtimeState, stage string, detailBuilder func(*ffmpegRealtimeState) string) {
	if line == "" {
		return
	}

	key, value, ok := strings.Cut(line, "=")
	if !ok {
		return
	}

	if key == "progress" {
		r.emitFFmpegRealtimeProgress(strings.TrimSpace(value), state, stage, detailBuilder)
		return
	}

	state.mu.Lock()
	switch key {
	case "frame":
		state.frame = value
	case "fps":
		state.fps = value
	case "out_time":
		state.outTime = value
	case "out_time_ms":
		if parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64); err == nil {
			state.outTimeMS = parsed
			if !state.hasFirstOutTime {
				state.firstOutTimeMS = parsed
				state.hasFirstOutTime = true
			}
		}
	case "speed":
		state.speed = value
	case "total_size":
		state.totalSize = value
	}
	state.mu.Unlock()
}

// emitFFmpegRealtimeProgress 会把当前 FFmpeg 实时状态转换成对外进度日志。
func (r *screenshotRunner) emitFFmpegRealtimeProgress(status string, state *ffmpegRealtimeState, stage string, detailBuilder func(*ffmpegRealtimeState) string) {
	if status == "" {
		return
	}

	state.mu.Lock()
	defer state.mu.Unlock()

	percent := r.ffmpegProgressPercent(stage, status, state)
	detail := detailBuilder(state)
	if percent == state.lastLoggedPercent && detail == state.lastLoggedDetail {
		return
	}

	r.logProgressPercent(stage, percent, detail)
	state.lastLoggedPercent = percent
	state.lastLoggedDetail = detail
}

// ffmpegProgressPercent 会根据阶段和实时指标估算 FFmpeg 当前完成百分比。
func (r *screenshotRunner) ffmpegProgressPercent(stage, status string, state *ffmpegRealtimeState) float64 {
	if status == "end" {
		return 100
	}

	if stage == "字幕" {
		if processedSeconds, totalSeconds, ok := r.ffmpegSubtitleProcessedWindow(state); ok && totalSeconds > 0 {
			percent := processedSeconds / totalSeconds * 100
			if percent < 0.1 {
				percent = 0.1
			}
			return clampProgressPercent(minFloat(percent, 94))
		}
	}

	if stage == "渲染" {
		if percent, ok := approximateRenderProgressPercent(state); ok {
			return percent
		}
	}

	state.heartbeatCount++
	percent := 12 + state.heartbeatCount*8
	if strings.TrimSpace(state.speed) != "" {
		if percent < 26 {
			percent = 26
		}
	}
	if state.outTimeMS > 0 || strings.TrimSpace(state.totalSize) != "" {
		if percent < 48 {
			percent = 48
		}
	}
	if frame, err := strconv.Atoi(strings.TrimSpace(state.frame)); err == nil && frame > 0 {
		if percent < 78 {
			percent = 78
		}
	}
	return clampProgressPercent(minFloat(float64(percent), 94))
}

// approximateRenderProgressPercent 会优先根据输出时间或速度估算单帧渲染进度。
func approximateRenderProgressPercent(state *ffmpegRealtimeState) (float64, bool) {
	if state == nil || state.windowSeconds <= 0 {
		if percent, ok := approximateUnknownRenderProgressPercent(state); ok {
			return percent, true
		}
		return 0, false
	}

	if state.hasFirstOutTime && state.outTimeMS > state.firstOutTimeMS {
		processedSeconds := float64(state.outTimeMS-state.firstOutTimeMS) / 1_000_000.0
		if processedSeconds > 0 {
			percent := processedSeconds / state.windowSeconds * 100
			if percent < 0.1 {
				percent = 0.1
			}
			return clampProgressPercent(minFloat(percent, 94)), true
		}
	}

	speed, ok := parseFFmpegSpeed(state.speed)
	if !ok || speed <= 0 {
		return 0, false
	}
	elapsed := time.Since(state.startedAt).Seconds()
	if elapsed <= 0 {
		return 0, false
	}
	estimatedTotal := state.windowSeconds / speed
	if estimatedTotal <= 0 {
		if percent, ok := approximateUnknownRenderProgressPercent(state); ok {
			return percent, true
		}
		return 0, false
	}
	percent := elapsed / estimatedTotal * 100
	if percent < 0.1 {
		percent = 0.1
	}
	return clampProgressPercent(minFloat(percent, 94)), true
}

// approximateUnknownRenderProgressPercent 会在缺少稳定指标时用耗时平滑估算渲染进度。
func approximateUnknownRenderProgressPercent(state *ffmpegRealtimeState) (float64, bool) {
	if state == nil || state.startedAt.IsZero() {
		return 0, false
	}
	elapsed := time.Since(state.startedAt).Seconds()
	if elapsed <= 0 {
		return 0, false
	}

	// 单帧截图经常拿不到稳定的 ffmpeg 实时指标，这里用一个平滑的
	// elapsed-time 估算，让进度条持续前进但不会很快冲到头。
	estimate := 1.5
	if state.windowSeconds > 0 {
		estimate = maxFloat(estimate, minFloat(state.windowSeconds, 3.0))
	}

	percent := 94.0 * elapsed / (elapsed + estimate)
	if percent < 0.1 {
		percent = 0.1
	}
	return clampProgressPercent(percent), true
}

// ffmpegRenderProgressDetail 会生成截图渲染阶段的实时进度文案。
func (r *screenshotRunner) ffmpegRenderProgressDetail(state *ffmpegRealtimeState) string {
	base := r.activeRenderProgressLabel()
	return base + r.ffmpegProgressMetricsSuffix(state)
}

// ffmpegSubtitleProgressDetail 会生成字幕提取阶段的实时进度文案。
func (r *screenshotRunner) ffmpegSubtitleProgressDetail(state *ffmpegRealtimeState) string {
	base := "正在提取内封文字字幕。"
	if processedSeconds, totalSeconds, ok := r.ffmpegSubtitleProcessedWindow(state); ok {
		return fmt.Sprintf("%s | 已处理 %s / %s", base, secToHMS(processedSeconds), secToHMS(totalSeconds))
	}
	return base + r.ffmpegProgressMetricsSuffix(state)
}

// ffmpegSubtitleProcessedWindow 会把字幕提取实时状态换算成适合展示的已处理时长和总时长。
func (r *screenshotRunner) ffmpegSubtitleProcessedWindow(state *ffmpegRealtimeState) (float64, float64, bool) {
	if r == nil || state == nil || r.media.Duration <= 0 || state.outTimeMS <= 0 {
		return 0, 0, false
	}

	totalSeconds := r.media.Duration
	processedSeconds := float64(state.outTimeMS) / 1_000_000.0
	firstSeconds := float64(state.firstOutTimeMS) / 1_000_000.0

	if state.hasFirstOutTime && state.firstOutTimeMS > 0 && firstSeconds < totalSeconds {
		processedSeconds = float64(maxInt64(state.outTimeMS-state.firstOutTimeMS, 0)) / 1_000_000.0
		totalSeconds -= firstSeconds
	}
	if totalSeconds <= 0 {
		totalSeconds = r.media.Duration
	}
	if processedSeconds < 0 {
		processedSeconds = 0
	}
	if processedSeconds > totalSeconds {
		processedSeconds = totalSeconds
	}
	return processedSeconds, totalSeconds, totalSeconds > 0
}

// ffmpegProgressMetricsSuffix 会把 frame、fps、speed 等指标拼接成进度详情后缀。
func (r *screenshotRunner) ffmpegProgressMetricsSuffix(state *ffmpegRealtimeState) string {
	parts := make([]string, 0, 4)
	if isUsefulFFmpegFrame(state.frame) {
		parts = append(parts, "frame="+strings.TrimSpace(state.frame))
	}
	if isUsefulFFmpegFPS(state.fps) {
		parts = append(parts, "fps="+strings.TrimSpace(state.fps))
	}
	if strings.TrimSpace(state.outTime) != "" && r.activeShot.Phase() == "" {
		parts = append(parts, "time="+strings.TrimSpace(state.outTime))
	}
	if isUsefulFFmpegSpeed(state.speed) {
		parts = append(parts, "speed="+strings.TrimSpace(state.speed))
	}
	if len(parts) == 0 {
		return ""
	}
	return " | " + strings.Join(parts, " | ")
}

// activeRenderProgressLabel 会返回当前截图渲染阶段适合展示的说明文本。
func (r *screenshotRunner) activeRenderProgressLabel() string {
	if r == nil {
		return "正在渲染截图。"
	}
	return r.activeShot.ProgressLabel()
}

// logShotAlignmentProgress 会记录当前截图进入字幕对齐阶段的进度文案。
func (r *screenshotRunner) logShotAlignmentProgress() {
	if r == nil || !r.activeShot.Active() {
		return
	}
	r.logProgress("截图开始", r.activeShot.Current(), r.activeShot.Total(), r.activeShot.AlignmentDetail())
}

// logBitmapSubtitleVisibilityProgress 会记录当前截图进入位图字幕可见性校验阶段的进度文案。
func (r *screenshotRunner) logBitmapSubtitleVisibilityProgress() {
	if r == nil || !r.activeShot.Active() {
		return
	}
	label := "PGS/DVD"
	switch {
	case r.isPGSSubtitle():
		label = "PGS"
	case r.isDVDSubtitle():
		label = "DVD"
	}
	r.logProgress("截图开始", r.activeShot.Current(), r.activeShot.Total(), r.activeShot.BitmapVisibilityDetail(label))
}

// displayAspectFilter 返回当前截图任务应使用的显示宽高比修正过滤器链。
func (r *screenshotRunner) displayAspectFilter() string {
	if strings.TrimSpace(r.render.AspectChain) != "" {
		return r.render.AspectChain
	}
	return buildDisplayAspectFilter()
}

// bitmapSubtitleTargetSize 会返回位图字幕叠加阶段需要匹配的目标画面尺寸。
func (r *screenshotRunner) bitmapSubtitleTargetSize() (int, int) {
	if r == nil {
		return 0, 0
	}
	if r.media.DisplayWidth > 0 && r.media.DisplayHeight > 0 {
		return r.media.DisplayWidth, r.media.DisplayHeight
	}
	return r.media.VideoWidth, r.media.VideoHeight
}

// hasUsablePGSCanvas 会判断当前是否拿到了可用于全画布叠加的 PGS 画布尺寸。
func (r *screenshotRunner) hasUsablePGSCanvas() bool {
	if r == nil || !r.isPGSSubtitle() {
		return false
	}
	targetWidth, targetHeight := r.bitmapSubtitleTargetSize()
	return r.render.SubtitleCanvasWidth > 0 && r.render.SubtitleCanvasHeight > 0 && targetWidth > 0 && targetHeight > 0
}

// buildPGSSubtitleScaleChain 会按目标画面尺寸缩放 PGS 画布。
func (r *screenshotRunner) buildPGSSubtitleScaleChain() string {
	if !r.hasUsablePGSCanvas() {
		return ""
	}
	targetWidth, targetHeight := r.bitmapSubtitleTargetSize()
	if targetWidth == r.render.SubtitleCanvasWidth && targetHeight == r.render.SubtitleCanvasHeight {
		return ""
	}
	return fmt.Sprintf("scale=%d:%d", targetWidth, targetHeight)
}

// pgsOverlayPosition 会返回当前 PGS 叠加使用的位置表达式。
func (r *screenshotRunner) pgsOverlayPosition() string {
	if r.hasUsablePGSCanvas() {
		return "0:0"
	}
	return "(W-w)/2:(H-h-10)"
}

// buildPGSOverlayFilterComplex 会构造“先处理视频，再叠加 PGS”的滤镜图。
func (r *screenshotRunner) buildPGSOverlayFilterComplex(videoChain, overlayTail string) string {
	steps := []string{
		buildFilterGraphStep("[0:v:0]", videoChain, "[video]"),
		buildFilterGraphStep(fmt.Sprintf("[0:s:%d]", r.subtitle.RelativeIndex), r.buildPGSSubtitleScaleChain(), "[sub]"),
		buildFilterGraphStep("[video][sub]", joinFilters(fmt.Sprintf("overlay=%s", r.pgsOverlayPosition()), overlayTail), "[out]"),
	}
	return strings.Join(steps, ";")
}

// buildPGSRenderFilterComplex 会构造截图主流程使用的 PGS 叠加滤镜图。
func (r *screenshotRunner) buildPGSRenderFilterComplex() string {
	return r.buildPGSOverlayFilterComplex(joinFilters(r.render.ColorChain, r.displayAspectFilter()), "")
}

// buildFilterGraphStep 会为 filter_complex 生成单个具名步骤。
func buildFilterGraphStep(input, chain, output string) string {
	filterChain := strings.TrimSpace(chain)
	if filterChain == "" {
		filterChain = "null"
	}
	return fmt.Sprintf("%s%s%s", input, filterChain, output)
}

// normalizeRenderProgressWindow 会把渲染窗口时长归一化到更稳定的估算范围。
func normalizeRenderProgressWindow(seconds float64) float64 {
	switch {
	case seconds <= 0:
		return 0.5
	case seconds < 0.5:
		return 0.5
	default:
		return seconds
	}
}

// minFloat 会返回两个浮点数中的较小值。
func minFloat(left, right float64) float64 {
	if left < right {
		return left
	}
	return right
}

// maxFloat 会返回两个浮点数中的较大值。
func maxFloat(left, right float64) float64 {
	if left > right {
		return left
	}
	return right
}

// maxInt64 会返回两个 int64 中的较大值。
func maxInt64(left, right int64) int64 {
	if left > right {
		return left
	}
	return right
}

// isUsefulFFmpegFrame 会判断 frame 指标是否可用于进度展示。
func isUsefulFFmpegFrame(raw string) bool {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return false
	}
	value, err := strconv.Atoi(trimmed)
	return err == nil && value > 0
}

// isUsefulFFmpegFPS 会判断 fps 指标是否可用于进度展示。
func isUsefulFFmpegFPS(raw string) bool {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || strings.EqualFold(trimmed, "n/a") {
		return false
	}
	value, err := strconv.ParseFloat(trimmed, 64)
	return err == nil && value > 0
}

// isUsefulFFmpegSpeed 会判断 speed 指标是否可用于进度展示。
func isUsefulFFmpegSpeed(raw string) bool {
	speed, ok := parseFFmpegSpeed(raw)
	return ok && speed > 0
}

// parseFFmpegSpeed 会把形如 2.3x 的 speed 文本解析成浮点倍速。
func parseFFmpegSpeed(raw string) (float64, bool) {
	trimmed := strings.TrimSpace(strings.TrimSuffix(raw, "x"))
	if trimmed == "" {
		return 0, false
	}
	value, err := strconv.ParseFloat(trimmed, 64)
	if err != nil || value <= 0 {
		return 0, false
	}
	return value, true
}

// buildTextSubtitleRenderChain 会让文字字幕渲染与视频选帧共享同一条绝对时间轴。
func (r *screenshotRunner) buildTextSubtitleRenderChain(timelineBase, aligned float64, subFilter string) string {
	baseTimeline := fmt.Sprintf("setpts=PTS-STARTPTS+%s/TB", formatFloat(timelineBase))
	selectFrame := fmt.Sprintf("select='gte(t,%s)'", formatFloat(aligned))
	if r.usesLibplaceboColorspace() {
		return joinFilters(
			baseTimeline,
			selectFrame,
			r.render.ColorChain,
			subFilter,
			r.displayAspectFilter(),
		)
	}
	return joinFilters(
		baseTimeline,
		selectFrame,
		subFilter,
		r.render.ColorChain,
		r.displayAspectFilter(),
	)
}

// buildTextSubtitleFilter 构建 ffmpeg 文本字幕过滤器，适配外挂字幕和内封文字字幕两种场景。
func (r *screenshotRunner) buildTextSubtitleFilter() string {
	if r.subtitle.Mode == "none" {
		return ""
	}

	sizePart := ""
	if r.media.VideoWidth > 0 && r.media.VideoHeight > 0 {
		sizePart = fmt.Sprintf(":original_size=%dx%d", r.media.VideoWidth, r.media.VideoHeight)
	}
	fontPart := ""
	if strings.TrimSpace(r.subtitleState.SubtitleFontDir) != "" {
		fontPart = fmt.Sprintf(":fontsdir='%s'", escapeFilterValue(r.subtitleState.SubtitleFontDir))
	}

	switch r.subtitle.Mode {
	case "external":
		return fmt.Sprintf("subtitles='%s'%s%s", escapeFilterValue(r.subtitle.File), sizePart, fontPart)
	case "internal":
		return fmt.Sprintf("subtitles='%s'%s%s:si=%d", escapeFilterValue(r.sourcePath), sizePart, fontPart, r.subtitle.RelativeIndex)
	default:
		return ""
	}
}

// bitmapSubtitleKind 返回当前字幕 codec 对应的位图字幕类型。
func (r *screenshotRunner) bitmapSubtitleKind() bitmapSubtitleKind {
	return bitmapSubtitleKindFromCodec(r.subtitle.Codec)
}

// isPGSSubtitle 会判断PGS字幕是否满足当前条件。
func (r *screenshotRunner) isPGSSubtitle() bool {
	return r.bitmapSubtitleKind() == bitmapSubtitlePGS
}

// isDVDSubtitle 会判断DVD字幕是否满足当前条件。
func (r *screenshotRunner) isDVDSubtitle() bool {
	return r.bitmapSubtitleKind() == bitmapSubtitleDVD
}

// isSupportedBitmapSubtitle 会判断受支持位图字幕是否满足当前条件。
func (r *screenshotRunner) isSupportedBitmapSubtitle() bool {
	return r.isPGSSubtitle() || r.isDVDSubtitle()
}

// requiresTextSubtitleFilter 会判断当前截图场景是否需要走 subtitles 文字滤镜。
func (r *screenshotRunner) requiresTextSubtitleFilter() bool {
	if r == nil || r.subtitle.Mode == "none" {
		return false
	}
	if r.subtitle.Mode == "external" {
		return true
	}
	if r.subtitle.Mode == "internal" && !r.isSupportedBitmapSubtitle() {
		return true
	}
	return false
}

// bitmapSubtitleKindFromCodec 把 codec 名称映射到内部使用的位图字幕类型枚举。
func bitmapSubtitleKindFromCodec(codec string) bitmapSubtitleKind {
	switch strings.ToLower(strings.TrimSpace(codec)) {
	case "hdmv_pgs_subtitle", "pgssub":
		return bitmapSubtitlePGS
	case "dvd_subtitle":
		return bitmapSubtitleDVD
	default:
		return bitmapSubtitleNone
	}
}

// isUnsupportedBitmapSubtitleCodec 会判断Unsupported位图字幕Codec是否满足当前条件。
func isUnsupportedBitmapSubtitleCodec(codec string) bool {
	switch strings.ToLower(strings.TrimSpace(codec)) {
	case "dvb_subtitle", "xsub", "vobsub":
		return true
	default:
		return false
	}
}
