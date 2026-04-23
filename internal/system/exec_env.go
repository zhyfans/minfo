// Package system 提供外部命令执行和实时输出转发能力。

package system

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"minfo/internal/config"
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
