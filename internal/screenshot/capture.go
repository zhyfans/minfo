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
