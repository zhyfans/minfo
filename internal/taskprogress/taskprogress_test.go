package taskprogress

import "testing"

// TestFormatStepAndParseLogLine 验证 step 型进度日志可被统一格式化并解析回结构化事件。
func TestFormatStepAndParseLogLine(t *testing.T) {
	line := FormatStep(StageSubtitle, 1, 3, "正在扫描全片字幕索引。")

	event, ok := ParseLogLine(line)
	if !ok {
		t.Fatal("ParseLogLine() returned ok=false")
	}
	if event.Kind != KindStep {
		t.Fatalf("Kind = %q, want %q", event.Kind, KindStep)
	}
	if event.Stage != StageSubtitle {
		t.Fatalf("Stage = %q, want %q", event.Stage, StageSubtitle)
	}
	if event.Current != 1 || event.Total != 3 {
		t.Fatalf("Current/Total = %d/%d, want 1/3", event.Current, event.Total)
	}
	if event.Detail != "正在扫描全片字幕索引。" {
		t.Fatalf("Detail = %q, want subtitle detail", event.Detail)
	}
}

// TestFormatPercentAndParseLogLine 验证 percent 型进度日志可被统一格式化并解析回结构化事件。
func TestFormatPercentAndParseLogLine(t *testing.T) {
	line := FormatPercent(StageRender, 47, "正在渲染第 1/4 张截图：00_05_41.png")

	event, ok := ParseLogLine(line)
	if !ok {
		t.Fatal("ParseLogLine() returned ok=false")
	}
	if event.Kind != KindPercent {
		t.Fatalf("Kind = %q, want %q", event.Kind, KindPercent)
	}
	if event.Stage != StageRender {
		t.Fatalf("Stage = %q, want %q", event.Stage, StageRender)
	}
	if event.Percent != 47 {
		t.Fatalf("Percent = %.1f, want 47", event.Percent)
	}
	if event.Detail != "正在渲染第 1/4 张截图：00_05_41.png" {
		t.Fatalf("Detail = %q, want render detail", event.Detail)
	}
}
