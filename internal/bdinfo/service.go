// Package bdinfo 负责调用 BDInfo 可执行文件并整理输出结果。

package bdinfo

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"minfo/internal/config"
	"minfo/internal/media"
	"minfo/internal/system"
)

const (
	defaultBinaryPath = "/opt/bdinfo/BDInfo"
	defaultReportName = "bdinfo.txt"
)

// RunOptions 定义 BDInfo 执行过程中的可选日志回调。
type RunOptions struct {
	CommandOutput system.OutputLineHandler
	Logf          func(format string, args ...any)
}

// Result 表示一次 BDInfo 运行返回的最终结果。
type Result struct {
	ResolvedPath string
	Output       string
}

type stagedSource struct {
	workDir   string
	scanInput string
	cleanup   func()
}

// Run 会执行完整的 BDInfo 处理流程，包括源路径准备、命令调用和报告整理。
func Run(ctx context.Context, inputPath string, options RunOptions) (Result, error) {
	resolvedPath, cleanup, err := media.ResolveBDInfoSource(ctx, inputPath)
	if err != nil {
		return Result{}, err
	}
	defer cleanup()
	options.logf("[bdinfo] 实际检测路径: %s", resolvedPath)

	binaryPath, err := resolveBinary()
	if err != nil {
		return Result{}, err
	}
	options.logf("[bdinfo] 使用命令: %s", binaryPath)

	staged, err := stageSource(ctx, resolvedPath, options)
	if err != nil {
		return Result{}, err
	}
	defer staged.cleanup()

	args, err := buildCommandArgs(staged.scanInput)
	if err != nil {
		return Result{}, err
	}
	options.logf("[bdinfo] 执行命令: cwd=%s | %s", staged.workDir, formatCommand(binaryPath, args...))

	stdout, stderr, err := system.RunCommandInDirLive(ctx, staged.workDir, binaryPath, options.CommandOutput, args...)
	if err != nil {
		return Result{}, fmt.Errorf(system.BestErrorMessage(err, stderr, stdout))
	}

	reportPath, err := findReportFile(staged.workDir)
	if err != nil {
		message := strings.TrimSpace(system.CombineCommandOutput(stdout, stderr))
		if message != "" {
			return Result{}, fmt.Errorf("%s\n\n%s", err.Error(), message)
		}
		return Result{}, err
	}
	options.logf("[bdinfo] 输出报告: %s", reportPath)

	reportBytes, err := os.ReadFile(reportPath)
	if err != nil {
		return Result{}, err
	}

	output := SelectLargestPlaylistBlock(string(reportBytes))
	if strings.TrimSpace(output) == "" {
		output = string(reportBytes)
	}

	return Result{
		ResolvedPath: resolvedPath,
		Output:       output,
	}, nil
}

// logf 会在可选日志回调存在时写入一条格式化日志。
func (o RunOptions) logf(format string, args ...any) {
	if o.Logf == nil {
		return
	}
	o.Logf(format, args...)
}

// resolveBinary 按环境变量和默认目录查找可用的 BDInfo 可执行文件。
func resolveBinary() (string, error) {
	envValue := strings.TrimSpace(os.Getenv("BDINFO_BIN"))
	if envValue != "" {
		if _, err := exec.LookPath(envValue); err != nil {
			return "", fmt.Errorf("%s not found; set BDINFO_BIN or install BDInfo under /opt/bdinfo", envValue)
		}
		return envValue, nil
	}

	if _, err := exec.LookPath(defaultBinaryPath); err == nil {
		return defaultBinaryPath, nil
	}

	candidates := make([]string, 0, 4)
	walkErr := filepath.WalkDir("/opt/bdinfo", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			rel, relErr := filepath.Rel("/opt/bdinfo", path)
			if relErr == nil && rel != "." && strings.Count(rel, string(filepath.Separator)) >= 4 {
				return filepath.SkipDir
			}
			return nil
		}

		base := strings.ToLower(strings.TrimSpace(d.Name()))
		if !strings.HasPrefix(base, "bdinfo") {
			return nil
		}

		info, infoErr := d.Info()
		if infoErr != nil {
			return infoErr
		}
		if info.Mode()&0o111 == 0 {
			return nil
		}
		candidates = append(candidates, path)
		return nil
	})
	if walkErr != nil && !errors.Is(walkErr, fs.ErrNotExist) {
		return "", walkErr
	}
	if len(candidates) == 0 {
		return "", errors.New("bdinfo: BDInfo binary not found under /opt/bdinfo")
	}
	sort.Strings(candidates)
	return candidates[0], nil
}

// buildCommandArgs 会构建命令参数，为后续流程准备好可直接使用的结果。
func buildCommandArgs(scanInput string) ([]string, error) {
	args := []string{"-p", scanInput, "-o", defaultReportName}

	extraArgs, err := splitCommandArgs(strings.TrimSpace(os.Getenv("BDINFO_ARGS")))
	if err != nil {
		return nil, fmt.Errorf("invalid BDINFO_ARGS: %w", err)
	}
	return append(args, extraArgs...), nil
}

// splitCommandArgs 以接近 shell 的规则拆分 BDINFO_ARGS，并保留引号和转义语义。
func splitCommandArgs(raw string) ([]string, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}

	args := make([]string, 0, 8)
	var current strings.Builder
	var quote byte
	escaped := false

	flushCurrent := func() {
		if current.Len() == 0 {
			return
		}
		args = append(args, current.String())
		current.Reset()
	}

	for index := 0; index < len(raw); index++ {
		ch := raw[index]

		if escaped {
			current.WriteByte(ch)
			escaped = false
			continue
		}

		switch ch {
		case '\\':
			if quote == '\'' {
				current.WriteByte(ch)
				continue
			}
			escaped = true
		case '\'', '"':
			if quote == 0 {
				quote = ch
				continue
			}
			if quote == ch {
				quote = 0
				continue
			}
			current.WriteByte(ch)
		case ' ', '\t', '\n', '\r':
			if quote != 0 {
				current.WriteByte(ch)
				continue
			}
			flushCurrent()
		default:
			current.WriteByte(ch)
		}
	}

	if escaped {
		return nil, errors.New("dangling escape")
	}
	if quote != 0 {
		return nil, errors.New("unterminated quote")
	}
	flushCurrent()
	return args, nil
}

// stageSource 为 BDInfo 准备工作目录和扫描目标，并返回对应的清理函数。
func stageSource(ctx context.Context, sourcePath string, options RunOptions) (stagedSource, error) {
	workDir, err := os.MkdirTemp("", "minfo-bdinfo-*")
	if err != nil {
		return stagedSource{}, err
	}

	cleanupFns := make([]func(), 0, 2)
	cleanup := func() {
		for index := len(cleanupFns) - 1; index >= 0; index-- {
			cleanupFns[index]()
		}
		_ = os.RemoveAll(workDir)
	}

	stage := stagedSource{
		workDir:   workDir,
		scanInput: sourcePath,
		cleanup:   cleanup,
	}

	sourceBDMV := detectBDMVSource(sourcePath)
	if sourceBDMV != "" {
		bindDir := filepath.Join(workDir, "source")
		bindTarget := filepath.Join(bindDir, "BDMV")
		if err := os.MkdirAll(bindTarget, 0o755); err != nil {
			cleanup()
			return stagedSource{}, err
		}
		if mounted, mountCleanup, mountErr := bindMountPath(ctx, sourceBDMV, bindTarget); mountErr == nil && mounted {
			cleanupFns = append(cleanupFns, mountCleanup)
			stage.scanInput = bindDir
			options.logf("[bdinfo] 已用 bind 挂载包装 BDMV 根: %s -> %s", sourceBDMV, bindTarget)
			return stage, nil
		}
		if err := os.Remove(bindTarget); err == nil {
			if err := os.Symlink(sourceBDMV, bindTarget); err == nil {
				stage.scanInput = bindDir
				options.logf("[bdinfo] bind 挂载不可用，已回退软链接包装 BDMV 根: %s -> %s", sourceBDMV, bindTarget)
				return stage, nil
			}
		}
		options.logf("[bdinfo] 包装 BDMV 根失败，直接使用原路径: %s", sourcePath)
		return stage, nil
	}

	info, err := os.Stat(sourcePath)
	if err != nil {
		cleanup()
		return stagedSource{}, err
	}
	if !info.IsDir() {
		return stage, nil
	}

	bindDir := filepath.Join(workDir, "source")
	if err := os.MkdirAll(bindDir, 0o755); err != nil {
		cleanup()
		return stagedSource{}, err
	}
	if mounted, mountCleanup, mountErr := bindMountPath(ctx, sourcePath, bindDir); mountErr == nil && mounted {
		cleanupFns = append(cleanupFns, mountCleanup)
		stage.scanInput = bindDir
		options.logf("[bdinfo] 已用 bind 挂载包装输入目录: %s -> %s", sourcePath, bindDir)
		return stage, nil
	}

	linkPath := filepath.Join(bindDir, "link")
	if err := os.Symlink(sourcePath, linkPath); err == nil {
		stage.scanInput = linkPath
		options.logf("[bdinfo] bind 挂载不可用，已回退软链接包装输入目录: %s -> %s", sourcePath, linkPath)
		return stage, nil
	}

	options.logf("[bdinfo] 无法包装输入目录，直接使用原路径: %s", sourcePath)
	return stage, nil
}

// detectBDMVSource 返回可直接交给 BDInfo 的 BDMV 根目录；找不到时返回空字符串。
func detectBDMVSource(path string) string {
	if directoryExists(filepath.Join(path, "BDMV")) {
		return filepath.Join(path, "BDMV")
	}
	if directoryExists(path) && strings.EqualFold(filepath.Base(path), "BDMV") {
		return path
	}
	return ""
}

// directoryExists 判断路径是否存在且为目录。
func directoryExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// bindMountPath 将源目录只读绑定挂载到目标路径，并返回卸载清理函数。
func bindMountPath(ctx context.Context, sourcePath, targetPath string) (bool, func(), error) {
	mountBin, err := system.ResolveBin("MOUNT_BIN", "mount")
	if err != nil {
		return false, nil, err
	}
	umountBin, err := system.ResolveBin("UMOUNT_BIN", "umount")
	if err != nil {
		return false, nil, err
	}

	mountCtx, cancel := context.WithTimeout(ctx, config.MountTimeout)
	defer cancel()

	if _, stderr, err := system.RunCommand(mountCtx, mountBin, "--bind", sourcePath, targetPath); err != nil {
		message := strings.TrimSpace(stderr)
		if message == "" {
			message = err.Error()
		}
		return false, nil, errors.New(message)
	}

	cleanup := func() {
		umountCtx, cancel := context.WithTimeout(context.Background(), config.UmountTimeout)
		defer cancel()
		if _, _, err := system.RunCommand(umountCtx, umountBin, targetPath); err != nil {
			_, _, _ = system.RunCommand(umountCtx, umountBin, "-l", targetPath)
		}
	}
	return true, cleanup, nil
}

// findReportFile 会查找报告文件，并在多个候选项中返回最合适的结果。
func findReportFile(workDir string) (string, error) {
	reportPath := filepath.Join(workDir, defaultReportName)
	if info, err := os.Stat(reportPath); err == nil && !info.IsDir() {
		return reportPath, nil
	}

	type candidate struct {
		path    string
		modTime time.Time
	}

	candidates := make([]candidate, 0, 4)
	entries, err := os.ReadDir(workDir)
	if err != nil {
		return "", err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			continue
		}
		candidates = append(candidates, candidate{
			path:    filepath.Join(workDir, entry.Name()),
			modTime: info.ModTime(),
		})
	}
	if len(candidates) == 0 {
		return "", errors.New("bdinfo: no report file produced")
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].modTime.Equal(candidates[j].modTime) {
			return candidates[i].path < candidates[j].path
		}
		return candidates[i].modTime.After(candidates[j].modTime)
	})
	return candidates[0].path, nil
}

// formatCommand 把命令和参数拼成适合日志展示的可读字符串。
func formatCommand(bin string, args ...string) string {
	parts := make([]string, 0, len(args)+1)
	parts = append(parts, quoteArg(bin))
	for _, arg := range args {
		parts = append(parts, quoteArg(arg))
	}
	return strings.Join(parts, " ")
}

// quoteArg 会转义参数，避免命令或过滤器拼接时出现语义错误。
func quoteArg(value string) string {
	if value == "" {
		return `""`
	}
	if strings.ContainsAny(value, " \t\r\n\"'\\") {
		return strconvQuote(value)
	}
	return value
}

// strconvQuote 以 Go 风格的双引号字面量形式转义字符串。
func strconvQuote(value string) string {
	return fmt.Sprintf("%q", value)
}
