package system

import (
	"context"
	"runtime"
	"strings"
	"testing"

	"minfo/internal/config"
)

func TestLineRelayWriterSplitsOnCarriageReturn(t *testing.T) {
	lines := make([]string, 0, 4)
	writer := newLineRelayWriter("stdout", func(stream, line string) {
		lines = append(lines, stream+":"+line)
	})

	if _, err := writer.Write([]byte("Scanning  10% - 00001.M2TS\rScanning  42% - 00002.M2TS\r")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	if len(lines) != 2 {
		t.Fatalf("len(lines) = %d, want 2", len(lines))
	}
	if lines[0] != "stdout:Scanning  10% - 00001.M2TS" {
		t.Fatalf("lines[0] = %q, want first carriage-return line", lines[0])
	}
	if lines[1] != "stdout:Scanning  42% - 00002.M2TS" {
		t.Fatalf("lines[1] = %q, want second carriage-return line", lines[1])
	}
}

func TestLineRelayWriterHandlesCRLFWithoutDuplicateLine(t *testing.T) {
	lines := make([]string, 0, 4)
	writer := newLineRelayWriter("stdout", func(stream, line string) {
		lines = append(lines, line)
	})

	if _, err := writer.Write([]byte("line1\r\nline2\r\n")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	if len(lines) != 2 {
		t.Fatalf("len(lines) = %d, want 2", len(lines))
	}
	if lines[0] != "line1" || lines[1] != "line2" {
		t.Fatalf("lines = %#v, want [line1 line2]", lines)
	}
}

func TestCommandEnvInjectsGalliumCPUCapsForFFmpeg(t *testing.T) {
	prev := config.FFmpegSSECompat
	config.FFmpegSSECompat = true
	defer func() {
		config.FFmpegSSECompat = prev
	}()

	env := commandEnv(context.Background(), FFmpegBinaryPath)
	expected := detectFFmpegGalliumCPUCaps()
	found := ""
	for _, entry := range env {
		if strings.HasPrefix(entry, "GALLIUM_OVERRIDE_CPU_CAPS=") {
			found = strings.TrimPrefix(entry, "GALLIUM_OVERRIDE_CPU_CAPS=")
			break
		}
	}
	if found != expected {
		t.Fatalf("expected ffmpeg command env to inject GALLIUM_OVERRIDE_CPU_CAPS=%q, got %q", expected, found)
	}

	widthFound := ""
	for _, entry := range env {
		if strings.HasPrefix(entry, "LP_NATIVE_VECTOR_WIDTH=") {
			widthFound = strings.TrimPrefix(entry, "LP_NATIVE_VECTOR_WIDTH=")
			break
		}
	}
	if expected != "" && widthFound != ffmpegNativeVectorWidth {
		t.Fatalf("expected ffmpeg command env to inject LP_NATIVE_VECTOR_WIDTH=%q, got %q", ffmpegNativeVectorWidth, widthFound)
	}
	if expected == "" && widthFound != "" {
		t.Fatalf("did not expect LP_NATIVE_VECTOR_WIDTH without Gallium CPU caps, got %q", widthFound)
	}
}

func TestCommandEnvDoesNotInjectGalliumCPUCapsForFFmpegByDefault(t *testing.T) {
	prev := config.FFmpegSSECompat
	config.FFmpegSSECompat = false
	defer func() {
		config.FFmpegSSECompat = prev
	}()

	env := commandEnv(context.Background(), FFmpegBinaryPath)
	for _, entry := range env {
		if strings.HasPrefix(entry, "GALLIUM_OVERRIDE_CPU_CAPS=") || strings.HasPrefix(entry, "LP_NATIVE_VECTOR_WIDTH=") {
			t.Fatalf("did not expect ffmpeg env to inject SSE compatibility overrides by default, got %q", entry)
		}
	}
}

func TestCommandEnvDoesNotInjectGalliumCPUCapsForFFprobe(t *testing.T) {
	prev := config.FFmpegSSECompat
	config.FFmpegSSECompat = true
	defer func() {
		config.FFmpegSSECompat = prev
	}()

	env := commandEnv(context.Background(), FFprobeBinaryPath)
	for _, entry := range env {
		if strings.HasPrefix(entry, "GALLIUM_OVERRIDE_CPU_CAPS=") {
			t.Fatalf("did not expect ffprobe env to inject ffmpeg-only tuning env, got %q", entry)
		}
	}
}

func TestFormatCommandForLogIncludesGalliumCPUCapsForFFmpeg(t *testing.T) {
	prev := config.FFmpegSSECompat
	config.FFmpegSSECompat = true
	defer func() {
		config.FFmpegSSECompat = prev
	}()

	command := FormatCommandForLog(context.Background(), FFmpegBinaryPath, "-hide_banner", "-version")
	expected := detectFFmpegGalliumCPUCaps()
	if expected != "" && !strings.Contains(command, "GALLIUM_OVERRIDE_CPU_CAPS="+expected) {
		t.Fatalf("expected command log to include GALLIUM_OVERRIDE_CPU_CAPS=%q, got %q", expected, command)
	}
	if expected != "" && !strings.Contains(command, "LP_NATIVE_VECTOR_WIDTH="+ffmpegNativeVectorWidth) {
		t.Fatalf("expected command log to include LP_NATIVE_VECTOR_WIDTH=%q, got %q", ffmpegNativeVectorWidth, command)
	}
	if expected == "" && strings.Contains(command, "GALLIUM_OVERRIDE_CPU_CAPS=") {
		t.Fatalf("did not expect command log to include GALLIUM_OVERRIDE_CPU_CAPS override, got %q", command)
	}
	if expected == "" && strings.Contains(command, "LP_NATIVE_VECTOR_WIDTH=") {
		t.Fatalf("did not expect command log to include LP_NATIVE_VECTOR_WIDTH override, got %q", command)
	}
	if !strings.Contains(command, FFmpegBinaryPath) {
		t.Fatalf("expected command log to include ffmpeg binary path, got %q", command)
	}
}

func TestFormatCommandForLogSkipsGalliumCPUCapsForFFmpegByDefault(t *testing.T) {
	prev := config.FFmpegSSECompat
	config.FFmpegSSECompat = false
	defer func() {
		config.FFmpegSSECompat = prev
	}()

	command := FormatCommandForLog(context.Background(), FFmpegBinaryPath, "-hide_banner", "-version")
	if strings.Contains(command, "GALLIUM_OVERRIDE_CPU_CAPS=") || strings.Contains(command, "LP_NATIVE_VECTOR_WIDTH=") {
		t.Fatalf("did not expect command log to include SSE compatibility overrides by default, got %q", command)
	}
}

func TestDetectFFmpegGalliumCPUCapsFromCPUInfoUsesSSE41Cap(t *testing.T) {
	cpuinfo := "flags\t: fpu sse sse2 sse3 ssse3 sse4_1 sse4_2 avx avx2\n"
	if got := detectFFmpegGalliumCPUCapsFromCPUInfo("amd64", cpuinfo); got != "sse4.1" {
		t.Fatalf("detectFFmpegGalliumCPUCapsFromCPUInfo() = %q, want %q", got, "sse4.1")
	}
}

func TestDetectFFmpegGalliumCPUCapsFromCPUInfoFallsBackForOlderX86(t *testing.T) {
	cpuinfo := "flags\t: fpu sse sse2 sse3 ssse3\n"
	if got := detectFFmpegGalliumCPUCapsFromCPUInfo("amd64", cpuinfo); got != "ssse3" {
		t.Fatalf("detectFFmpegGalliumCPUCapsFromCPUInfo() = %q, want %q", got, "ssse3")
	}
}

func TestDetectFFmpegGalliumCPUCapsFromCPUInfoSkipsNonX86(t *testing.T) {
	cpuinfo := "Features\t: fp asimd aes crc32\n"
	if got := detectFFmpegGalliumCPUCapsFromCPUInfo("arm64", cpuinfo); got != "" {
		t.Fatalf("detectFFmpegGalliumCPUCapsFromCPUInfo() = %q, want empty for non-x86", got)
	}
}

func TestDefaultFFmpegGalliumCPUCaps(t *testing.T) {
	if got := defaultFFmpegGalliumCPUCaps("amd64"); got != "sse2" {
		t.Fatalf("defaultFFmpegGalliumCPUCaps(amd64) = %q, want %q", got, "sse2")
	}
	if got := defaultFFmpegGalliumCPUCaps("arm64"); got != "" {
		t.Fatalf("defaultFFmpegGalliumCPUCaps(arm64) = %q, want empty", got)
	}
}

func TestDetectFFmpegGalliumCPUCapsMatchesCurrentArchitectureShape(t *testing.T) {
	got := detectFFmpegGalliumCPUCaps()
	switch runtime.GOARCH {
	case "amd64":
		if got == "" {
			t.Fatal("expected non-empty Gallium CPU caps on amd64")
		}
	default:
		// Non-x86 architectures should not inject the x86-only Gallium caps override.
		if got != "" && runtime.GOARCH != "386" {
			t.Fatalf("expected empty Gallium CPU caps on %s, got %q", runtime.GOARCH, got)
		}
	}
}
