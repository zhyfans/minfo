package handlers

import (
	"testing"

	"minfo/internal/httpapi/transport"
	"minfo/internal/screenshot"
)

func TestBuildInfoTaskProgressForMediaInfoRunning(t *testing.T) {
	progress := buildInfoTaskProgress(infoKindMediaInfo, infoJobStatusRunning, []transport.LogEntry{
		{Message: "[mediainfo] 输入路径: /media/demo"},
		{Message: "[mediainfo] 使用命令: /usr/bin/mediainfo"},
		{Message: "[mediainfo] 候选源数量: 3"},
		{Message: "[mediainfo] 尝试 2/3: /media/demo/STREAM/00002.m2ts"},
	})

	if progress != nil {
		t.Fatalf("progress = %#v, want nil for mediainfo", progress)
	}
}

func TestBuildInfoTaskProgressForBDInfoRunning(t *testing.T) {
	progress := buildInfoTaskProgress(infoKindBDInfo, infoJobStatusRunning, []transport.LogEntry{
		{Message: "[bdinfo] 实际检测路径: /media/demo"},
		{Message: "[bdinfo] 使用命令: /usr/local/bin/bdinfo"},
		{Message: "[bdinfo] 已用 bind 挂载包装 BDMV 根: /media/demo/BDMV -> /tmp/minfo/source/BDMV"},
		{Message: "[bdinfo] 执行命令: cwd=/tmp/minfo | /usr/local/bin/bdinfo -w /tmp/minfo/source /tmp/minfo"},
	})

	if progress == nil {
		t.Fatal("progress is nil")
	}
	if progress.Stage != "启动扫描" {
		t.Fatalf("Stage = %q, want %q", progress.Stage, "启动扫描")
	}
	if !progress.Indeterminate {
		t.Fatalf("Indeterminate = false, want true")
	}
	if progress.Percent < 20 || progress.Percent > 24 {
		t.Fatalf("Percent = %.2f, want around low-20s before real scan progress arrives", progress.Percent)
	}
}

func TestBuildInfoTaskProgressForBDInfoCLIRealScanProgress(t *testing.T) {
	progress := buildInfoTaskProgress(infoKindBDInfo, infoJobStatusRunning, []transport.LogEntry{
		{Message: "[bdinfo] 实际检测路径: /media/demo"},
		{Message: "[bdinfo] 使用命令: /usr/local/bin/bdinfo"},
		{Message: "[bdinfo] 执行命令: cwd=/tmp/minfo | /usr/local/bin/bdinfo -w /tmp/minfo/source /tmp/minfo"},
		{Message: "[bdinfo][stdout] Please wait while we scan the disc..."},
		{Message: "[bdinfo][stdout] Scanning  42% - 00010.M2TS     00:00:12  |  00:00:15"},
	})

	if progress == nil {
		t.Fatal("progress is nil")
	}
	if progress.Stage != "扫描蓝光目录" {
		t.Fatalf("Stage = %q, want %q", progress.Stage, "扫描蓝光目录")
	}
	if progress.Current != 42 || progress.Total != 100 {
		t.Fatalf("Current/Total = %d/%d, want 42/100", progress.Current, progress.Total)
	}
	if progress.Detail != "正在扫描蓝光目录：00010.M2TS     00:00:12  |  00:00:15" {
		t.Fatalf("Detail = %q, want parsed scan detail", progress.Detail)
	}
	if progress.Percent < 50 || progress.Percent > 52 {
		t.Fatalf("Percent = %.2f, want between 50 and 52", progress.Percent)
	}
	if progress.Indeterminate {
		t.Fatalf("Indeterminate = true, want false")
	}
}

func TestBuildInfoTaskProgressForBDInfoReportGeneration(t *testing.T) {
	progress := buildInfoTaskProgress(infoKindBDInfo, infoJobStatusRunning, []transport.LogEntry{
		{Message: "[bdinfo][stdout] Scan completed successfully."},
		{Message: "[bdinfo][stdout] Please wait while we generate the report..."},
	})

	if progress == nil {
		t.Fatal("progress is nil")
	}
	if progress.Stage != "生成报告" {
		t.Fatalf("Stage = %q, want %q", progress.Stage, "生成报告")
	}
	if progress.Percent < 88 || progress.Percent > 89 {
		t.Fatalf("Percent = %.2f, want around 88", progress.Percent)
	}
	if !progress.Indeterminate {
		t.Fatalf("Indeterminate = false, want true")
	}
}

func TestBuildScreenshotTaskProgressForZipRunning(t *testing.T) {
	progress := buildScreenshotTaskProgress(screenshot.ModeZip, screenshotJobStatusRunning, 4, []transport.LogEntry{
		{Message: "[信息] 容器起始偏移：0.000s | 影片总时长：01:45:20"},
		{Message: "[信息] 截图: 请求 00:10:00.000 → 对齐 00:10:01.000 → 输出 00:10:01.000 -> 00_10_01.png"},
		{Message: "[信息] 截图: 请求 00:30:00.000 → 对齐 00:30:01.000 → 输出 00:30:01.000 -> 00_30_01.png"},
	})

	if progress == nil {
		t.Fatal("progress is nil")
	}
	if progress.Stage != "生成截图" {
		t.Fatalf("Stage = %q, want %q", progress.Stage, "生成截图")
	}
	if progress.Current != 2 || progress.Total != 4 {
		t.Fatalf("Current/Total = %d/%d, want 2/4", progress.Current, progress.Total)
	}
	if progress.Percent <= 22 {
		t.Fatalf("Percent = %.2f, want > 22", progress.Percent)
	}
}

func TestBuildScreenshotTaskProgressForUploadRunning(t *testing.T) {
	progress := buildScreenshotTaskProgress(screenshot.ModeLinks, screenshotJobStatusRunning, 4, []transport.LogEntry{
		{Message: "[信息] 容器起始偏移：0.000s | 影片总时长：01:45:20"},
		{Message: "===== 任务完成 ====="},
		{Message: "开始处理 4 个文件..."},
		{Message: "已上传并校准域名: 00_10_01.png"},
		{Message: "上传失败: 00_30_01.png (timeout)"},
	})

	if progress == nil {
		t.Fatal("progress is nil")
	}
	if progress.Stage != "上传图床" {
		t.Fatalf("Stage = %q, want %q", progress.Stage, "上传图床")
	}
	if progress.Current != 2 || progress.Total != 4 {
		t.Fatalf("Current/Total = %d/%d, want 2/4", progress.Current, progress.Total)
	}
	if progress.Percent < 72 {
		t.Fatalf("Percent = %.2f, want >= 72", progress.Percent)
	}
}

func TestBuildScreenshotTaskProgressForSubtitleMarker(t *testing.T) {
	progress := buildScreenshotTaskProgress(screenshot.ModeZip, screenshotJobStatusRunning, 1, []transport.LogEntry{
		{Message: "[进度] 字幕 2/3: 正在用 ffprobe 补充蓝光字幕元数据：playlist 00800。"},
	})

	if progress == nil {
		t.Fatal("progress is nil")
	}
	if progress.Stage != "准备字幕" {
		t.Fatalf("Stage = %q, want %q", progress.Stage, "准备字幕")
	}
	if progress.Detail != "正在用 ffprobe 补充蓝光字幕元数据：playlist 00800。" {
		t.Fatalf("Detail = %q, want subtitle detail", progress.Detail)
	}
	if progress.Percent != 0 {
		t.Fatalf("Percent = %.2f, want 0 when subtitle prep has no time-based ffmpeg progress", progress.Percent)
	}
}

func TestBuildScreenshotTaskProgressForSubtitlePercentMarkerUsesStepProgress(t *testing.T) {
	progress := buildScreenshotTaskProgress(screenshot.ModeZip, screenshotJobStatusRunning, 1, []transport.LogEntry{
		{Message: "[进度] 字幕 3/3: 正在提取内挂文字字幕。"},
		{Message: "[进度] 字幕 50%: 正在提取内挂文字字幕。 | frame=12 | speed=1.0x"},
	})

	if progress == nil {
		t.Fatal("progress is nil")
	}
	if progress.Stage != "准备字幕" {
		t.Fatalf("Stage = %q, want %q", progress.Stage, "准备字幕")
	}
	if progress.Current != 3 || progress.Total != 3 {
		t.Fatalf("Current/Total = %d/%d, want 3/3", progress.Current, progress.Total)
	}
	if progress.Detail != "正在提取内挂文字字幕。 | frame=12 | speed=1.0x" {
		t.Fatalf("Detail = %q, want merged subtitle detail", progress.Detail)
	}
	if progress.Percent != 15 {
		t.Fatalf("Percent = %.2f, want 15 when subtitle extraction reaches 50%% of the reserved 30%%", progress.Percent)
	}
	if progress.Indeterminate {
		t.Fatalf("Indeterminate = true, want false")
	}
}

func TestBuildScreenshotTaskProgressForRenderPercentMarker(t *testing.T) {
	progress := buildScreenshotTaskProgress(screenshot.ModeZip, screenshotJobStatusRunning, 1, []transport.LogEntry{
		{Message: "[进度] 截图开始 1/1: 正在渲染第 1/1 张截图：00_10_01.png"},
		{Message: "[进度] 渲染 48%: 正在渲染第 1/1 张截图：00_10_01.png | frame=0 | speed=0.7x"},
	})

	if progress == nil {
		t.Fatal("progress is nil")
	}
	if progress.Stage != "生成截图" {
		t.Fatalf("Stage = %q, want %q", progress.Stage, "生成截图")
	}
	if progress.Detail != "正在渲染第 1/1 张截图：00_10_01.png | frame=0 | speed=0.7x" {
		t.Fatalf("Detail = %q, want render detail", progress.Detail)
	}
	if progress.Current != 1 || progress.Total != 1 {
		t.Fatalf("Current/Total = %d/%d, want 1/1", progress.Current, progress.Total)
	}
	if progress.Percent <= 34 || progress.Percent >= 84 {
		t.Fatalf("Percent = %.2f, want between 34 and 84", progress.Percent)
	}
}

func TestBuildScreenshotTaskProgressForRenderPercentMarkerDoesNotRollbackOnReencode(t *testing.T) {
	progress := buildScreenshotTaskProgress(screenshot.ModeZip, screenshotJobStatusRunning, 1, []transport.LogEntry{
		{Message: "[进度] 截图开始 1/1: 正在渲染第 1/1 张截图：00_10_01.png"},
		{Message: "[进度] 渲染 92%: 正在渲染第 1/1 张截图：00_10_01.png | frame=1 | speed=1.4x"},
		{Message: "[进度] 渲染 8%: 正在重拍第 1/1 张截图：00_10_01.png | frame=0 | speed=0.6x"},
	})

	if progress == nil {
		t.Fatal("progress is nil")
	}
	if progress.Stage != "生成截图" {
		t.Fatalf("Stage = %q, want %q", progress.Stage, "生成截图")
	}
	if progress.Detail != "正在重拍第 1/1 张截图：00_10_01.png | frame=0 | speed=0.6x" {
		t.Fatalf("Detail = %q, want latest render detail", progress.Detail)
	}
	if progress.Percent < 70 {
		t.Fatalf("Percent = %.2f, want >= 70 to avoid rollback after reencode restart", progress.Percent)
	}
}

func TestBuildScreenshotTaskProgressForPackageMarker(t *testing.T) {
	progress := buildScreenshotTaskProgress(screenshot.ModeZip, screenshotJobStatusRunning, 1, []transport.LogEntry{
		{Message: "[进度] 截图完成 1/1: 已完成第 1/1 张截图：00_10_01.png"},
		{Message: "[进度] 整理 2/4: 正在压缩截图文件。"},
	})

	if progress == nil {
		t.Fatal("progress is nil")
	}
	if progress.Stage != "整理结果" {
		t.Fatalf("Stage = %q, want %q", progress.Stage, "整理结果")
	}
	if progress.Detail != "正在压缩截图文件。" {
		t.Fatalf("Detail = %q, want %q", progress.Detail, "正在压缩截图文件。")
	}
	if progress.Current != 2 || progress.Total != 4 {
		t.Fatalf("Current/Total = %d/%d, want 2/4", progress.Current, progress.Total)
	}
	if progress.Percent < 84 {
		t.Fatalf("Percent = %.2f, want >= 84", progress.Percent)
	}
}
