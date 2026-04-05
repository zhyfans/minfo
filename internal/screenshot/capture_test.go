// Package screenshot 验证截图过滤器拼接逻辑的关键回归场景。

package screenshot

import (
	"strings"
	"testing"
)

// TestBuildTextSubtitleFilterForInternalTextSubtitle 验证内封文字字幕过滤器会保持与 shell 一致的 si 写法。
func TestBuildTextSubtitleFilterForInternalTextSubtitle(t *testing.T) {
	runner := &screenshotRunner{
		sourcePath:  "/media/example/video.mkv",
		videoWidth:  1920,
		videoHeight: 1080,
		subtitle: subtitleSelection{
			Mode:          "internal",
			RelativeIndex: 1,
		},
	}

	filter := runner.buildTextSubtitleFilter()
	if !strings.Contains(filter, "subtitles='/media/example/video.mkv'") {
		t.Fatalf("expected shell-style subtitles path in filter, got %q", filter)
	}
	if !strings.Contains(filter, ":si=1") {
		t.Fatalf("expected shell-style si option in filter, got %q", filter)
	}
	if !strings.Contains(filter, ":original_size=1920x1080") {
		t.Fatalf("expected original_size in filter, got %q", filter)
	}
}

// TestBuildTextSubtitleFilterForExternalSubtitle 验证外挂文字字幕过滤器也会保持 shell 的位置参数写法。
func TestBuildTextSubtitleFilterForExternalSubtitle(t *testing.T) {
	runner := &screenshotRunner{
		videoWidth:  1280,
		videoHeight: 720,
		subtitle: subtitleSelection{
			Mode: "external",
			File: "/media/example/subtitle.srt",
		},
	}

	filter := runner.buildTextSubtitleFilter()
	if !strings.Contains(filter, "subtitles='/media/example/subtitle.srt'") {
		t.Fatalf("expected shell-style subtitles path in filter, got %q", filter)
	}
	if strings.Contains(filter, ":si=") {
		t.Fatalf("did not expect si for external subtitle, got %q", filter)
	}
}

// TestShellStyleTextSubtitleChain 验证文字字幕过滤器链保持 shell 的 setpts 后接 subtitles 顺序。
func TestShellStyleTextSubtitleChain(t *testing.T) {
	filter := joinFilters(
		"setpts=PTS-STARTPTS,select='gte(t,1.000)'",
		"setpts=PTS-STARTPTS+61.000/TB",
		"subtitles='/media/example/video.mkv':original_size=3840x2160:si=1",
	)

	expected := "setpts=PTS-STARTPTS,select='gte(t,1.000)',setpts=PTS-STARTPTS+61.000/TB,subtitles='/media/example/video.mkv':original_size=3840x2160:si=1"
	if filter != expected {
		t.Fatalf("expected shell-style filter chain %q, got %q", expected, filter)
	}
}
