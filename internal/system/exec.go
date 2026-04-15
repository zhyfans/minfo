// Package system 提供外部命令执行和实时输出转发能力。

package system

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"minfo/internal/config"
)

const (
	FFmpegBinaryPath    = "/usr/bin/ffmpeg"
	FFprobeBinaryPath   = "/usr/bin/ffprobe"
	MediaInfoBinaryPath = "/usr/bin/mediainfo"
	OxiPNGBinaryPath    = "/usr/bin/oxipng"
	PNGQuantBinaryPath  = "/usr/bin/pngquant"
	BDInfoBinaryPath    = "/usr/local/bin/bdinfo"
	BDSubBinaryPath     = "/usr/local/bin/bdsub"
	MountBinaryPath     = "/bin/mount"
	UmountBinaryPath    = "/bin/umount"
	ModprobeBinaryPath  = "/sbin/modprobe"
)

const ffmpegNativeVectorWidth = "128"

type envOverride struct {
	key   string
	value string
}

var (
	ffmpegGalliumCPUCapsOnce  sync.Once
	ffmpegGalliumCPUCapsValue string
)

type ffmpegSSECompatContextKey struct{}

// FFmpegSSECompatEnabled 会判断当前上下文是否要求对 FFmpeg 注入 SSE 兼容环境变量。
func FFmpegSSECompatEnabled(ctx context.Context) bool {
	if ctx == nil {
		return config.FFmpegSSECompat
	}
	if enabled, ok := ctx.Value(ffmpegSSECompatContextKey{}).(bool); ok {
		return enabled
	}
	return config.FFmpegSSECompat
}

// ResolveBin 会校验固定路径的可执行文件当前可用，并返回该固定路径。
func ResolveBin(path string) (string, error) {
	if _, err := exec.LookPath(path); err != nil {
		return "", fmt.Errorf("%s not found", path)
	}
	return path, nil
}

// RunCommand 会在默认工作目录中执行外部命令，并返回完整 stdout、stderr 和错误状态。
func RunCommand(ctx context.Context, bin string, args ...string) (string, string, error) {
	return runCommand(ctx, "", bin, args...)
}

// RunCommandInDir 会在指定目录中执行外部命令，并返回完整 stdout、stderr 和错误状态。
func RunCommandInDir(ctx context.Context, dir, bin string, args ...string) (string, string, error) {
	return runCommand(ctx, dir, bin, args...)
}

// OutputLineHandler 处理实时命令输出的单行内容。
type OutputLineHandler func(stream, line string)

// RunCommandLive 会执行外部命令，并把输出按行实时转发给调用方。
func RunCommandLive(ctx context.Context, bin string, onLine OutputLineHandler, args ...string) (string, string, error) {
	return runCommandLive(ctx, "", bin, onLine, args...)
}

// RunCommandInDirLive 会在指定目录中执行外部命令，并把输出按行实时转发给调用方。
func RunCommandInDirLive(ctx context.Context, dir, bin string, onLine OutputLineHandler, args ...string) (string, string, error) {
	return runCommandLive(ctx, dir, bin, onLine, args...)
}

// runCommand 在指定目录启动命令，并完整收集 stdout 和 stderr。
func runCommand(ctx context.Context, dir, bin string, args ...string) (string, string, error) {
	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	cmd.Env = commandEnv(ctx, bin)
	setCommandProcessGroup(cmd)

	stdoutFile, err := os.CreateTemp("", "minfo-stdout-*")
	if err != nil {
		return "", "", err
	}
	defer os.Remove(stdoutFile.Name())
	defer stdoutFile.Close()

	stderrFile, err := os.CreateTemp("", "minfo-stderr-*")
	if err != nil {
		return "", "", err
	}
	defer os.Remove(stderrFile.Name())
	defer stderrFile.Close()

	cmd.Stdout = stdoutFile
	cmd.Stderr = stderrFile

	if err := cmd.Start(); err != nil {
		return "", "", err
	}

	waitCh := make(chan error, 1)
	go func() {
		waitCh <- cmd.Wait()
	}()

	var waitErr error
	select {
	case waitErr = <-waitCh:
	case <-ctx.Done():
		killCommandProcessGroup(cmd)
		waitErr = ctx.Err()
		<-waitCh
	}

	stdoutData, _ := os.ReadFile(stdoutFile.Name())
	stderrData, _ := os.ReadFile(stderrFile.Name())
	return string(stdoutData), string(stderrData), waitErr
}

// runCommandLive 在命令运行过程中按行转发输出，同时保留完整 stdout 和 stderr。
func runCommandLive(ctx context.Context, dir, bin string, onLine OutputLineHandler, args ...string) (string, string, error) {
	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	cmd.Env = commandEnv(ctx, bin)
	setCommandProcessGroup(cmd)

	var stdoutBuf bytes.Buffer
	var stderrBuf bytes.Buffer
	stdoutWriter := io.Writer(&stdoutBuf)
	stderrWriter := io.Writer(&stderrBuf)

	var stdoutRelay *lineRelayWriter
	var stderrRelay *lineRelayWriter
	if onLine != nil {
		stdoutRelay = newLineRelayWriter("stdout", onLine)
		stderrRelay = newLineRelayWriter("stderr", onLine)
		stdoutWriter = io.MultiWriter(&stdoutBuf, stdoutRelay)
		stderrWriter = io.MultiWriter(&stderrBuf, stderrRelay)
	}

	cmd.Stdout = stdoutWriter
	cmd.Stderr = stderrWriter

	if err := cmd.Start(); err != nil {
		return "", "", err
	}

	waitCh := make(chan error, 1)
	go func() {
		waitCh <- cmd.Wait()
	}()

	var waitErr error
	select {
	case waitErr = <-waitCh:
	case <-ctx.Done():
		killCommandProcessGroup(cmd)
		waitErr = ctx.Err()
		<-waitCh
	}

	if stdoutRelay != nil {
		stdoutRelay.Flush()
	}
	if stderrRelay != nil {
		stderrRelay.Flush()
	}

	return stdoutBuf.String(), stderrBuf.String(), waitErr
}

func commandEnv(ctx context.Context, bin string) []string {
	env := os.Environ()
	for _, override := range commandEnvOverrides(ctx, bin) {
		env = withEnvOverride(env, override.key, override.value)
	}
	return env
}

func commandEnvOverrides(ctx context.Context, bin string) []envOverride {
	if !shouldInjectFFmpegEnv(ctx, bin) {
		return nil
	}
	caps := detectFFmpegGalliumCPUCaps()
	if caps == "" {
		return nil
	}
	return []envOverride{
		{key: "LP_NATIVE_VECTOR_WIDTH", value: ffmpegNativeVectorWidth},
		{key: "GALLIUM_OVERRIDE_CPU_CAPS", value: caps},
	}
}

func detectFFmpegGalliumCPUCaps() string {
	ffmpegGalliumCPUCapsOnce.Do(func() {
		cpuinfo, err := os.ReadFile("/proc/cpuinfo")
		if err != nil {
			ffmpegGalliumCPUCapsValue = defaultFFmpegGalliumCPUCaps(runtime.GOARCH)
			return
		}
		ffmpegGalliumCPUCapsValue = detectFFmpegGalliumCPUCapsFromCPUInfo(runtime.GOARCH, string(cpuinfo))
	})
	return ffmpegGalliumCPUCapsValue
}

func detectFFmpegGalliumCPUCapsFromCPUInfo(goarch, cpuinfo string) string {
	switch goarch {
	case "amd64", "386":
		flags := parseCPUInfoFlags(cpuinfo)
		switch {
		case flags["sse4_1"]:
			return "sse4.1"
		case flags["ssse3"]:
			return "ssse3"
		case flags["sse3"]:
			return "sse3"
		case flags["sse2"]:
			return "sse2"
		default:
			return defaultFFmpegGalliumCPUCaps(goarch)
		}
	default:
		return ""
	}
}

func defaultFFmpegGalliumCPUCaps(goarch string) string {
	switch goarch {
	case "amd64":
		// amd64 guarantees SSE2, so this remains a safe minimum when cpuinfo is unavailable.
		return "sse2"
	default:
		return ""
	}
}

func parseCPUInfoFlags(cpuinfo string) map[string]bool {
	flags := make(map[string]bool)
	for _, line := range strings.Split(cpuinfo, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(strings.ToLower(line), "flags") {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		for _, field := range strings.Fields(strings.ToLower(parts[1])) {
			flags[field] = true
		}
	}
	return flags
}

func shouldInjectFFmpegEnv(ctx context.Context, bin string) bool {
	if !FFmpegSSECompatEnabled(ctx) {
		return false
	}
	trimmed := strings.TrimSpace(bin)
	if trimmed == "" {
		return false
	}
	base := filepath.Base(trimmed)
	return trimmed == FFmpegBinaryPath || base == "ffmpeg"
}

func withEnvOverride(base []string, key, value string) []string {
	prefix := key + "="
	merged := make([]string, 0, len(base)+1)
	for _, entry := range base {
		if strings.HasPrefix(entry, prefix) {
			continue
		}
		merged = append(merged, entry)
	}
	return append(merged, prefix+value)
}

// FormatCommandForLog 会把命令、参数和当前执行层注入的关键环境变量格式化成便于日志展示的字符串。
func FormatCommandForLog(ctx context.Context, bin string, args ...string) string {
	parts := make([]string, 0, len(args)+1+len(commandEnvOverrides(ctx, bin)))
	for _, override := range commandEnvOverrides(ctx, bin) {
		parts = append(parts, quoteArg(override.key+"="+override.value))
	}
	parts = append(parts, quoteArg(bin))
	for _, arg := range args {
		parts = append(parts, quoteArg(arg))
	}
	return strings.Join(parts, " ")
}

// quoteArg 会按 shell 友好的方式转义单个命令参数，便于日志直观展示。
func quoteArg(value string) string {
	if value == "" {
		return "''"
	}
	if strings.IndexFunc(value, func(r rune) bool {
		switch r {
		case ' ', '\t', '\n', '\r', '\'', '"', '\\', '|', '&', ';', '<', '>', '(', ')', '[', ']', '{', '}', '$':
			return true
		default:
			return false
		}
	}) == -1 {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

type lineRelayWriter struct {
	mu     sync.Mutex
	stream string
	onLine OutputLineHandler
	buffer bytes.Buffer
}

// newLineRelayWriter 创建一个按行拆分并转发输出的 Writer。
func newLineRelayWriter(stream string, onLine OutputLineHandler) *lineRelayWriter {
	return &lineRelayWriter{
		stream: stream,
		onLine: onLine,
	}
}

// Write 把命令输出写入缓冲区，并在读到完整行时立即回调。
func (w *lineRelayWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if _, err := w.buffer.Write(p); err != nil {
		return 0, err
	}
	w.flushCompleteLinesLocked()
	return len(p), nil
}

// Flush 会刷新缓冲内容，确保待发送数据及时写出。
func (w *lineRelayWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()

	remaining := strings.TrimRight(w.buffer.String(), "\r\n")
	if remaining != "" || w.buffer.Len() > 0 {
		w.onLine(w.stream, remaining)
	}
	w.buffer.Reset()
}

// flushCompleteLinesLocked 假定调用方已持锁，并把缓冲区中完整的行逐一回调出去。
func (w *lineRelayWriter) flushCompleteLinesLocked() {
	data := w.buffer.Bytes()
	start := 0
	for idx := 0; idx < len(data); idx++ {
		ch := data[idx]
		if ch != '\n' && ch != '\r' {
			continue
		}
		line := strings.TrimRight(string(data[start:idx]), "\r")
		w.onLine(w.stream, line)
		if ch == '\r' && idx+1 < len(data) && data[idx+1] == '\n' {
			start = idx + 2
			idx++
			continue
		}
		start = idx + 1
	}
	if start == 0 {
		return
	}
	w.buffer.Next(start)
}

// BestErrorMessage 组合 err、stderr 和 stdout，生成更适合展示给用户的错误文本。
func BestErrorMessage(err error, stderr, stdout string) string {
	msg := strings.TrimSpace(stderr)
	if msg == "" {
		msg = err.Error()
	}
	if strings.TrimSpace(stdout) != "" {
		msg += "\n\n" + strings.TrimSpace(stdout)
	}
	return msg
}

// CombineCommandOutput 合并 stdout 和 stderr，并保留两者的可读分隔。
func CombineCommandOutput(stdout, stderr string) string {
	output := strings.TrimSpace(stdout)
	if strings.TrimSpace(stderr) != "" {
		if output != "" {
			output += "\n\n"
		}
		output += strings.TrimSpace(stderr)
	}
	return output
}
