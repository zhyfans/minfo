// Package taskprogress 提供截图任务进度日志的统一格式与解析能力。

package taskprogress

// Prefix 是统一进度日志使用的固定前缀。
const Prefix = "[进度]"

// Stage 常量统一描述截图任务内部使用的阶段名称。
const (
	StageBootstrap    = "启动"
	StageSubtitle     = "字幕"
	StagePrepare      = "准备"
	StageCaptureStart = "截图开始"
	StageRender       = "渲染"
	StageCaptureDone  = "截图完成"
	StagePackage      = "整理"
	StageUpload       = "上传"
)

// Kind 表示一条进度日志的载荷类型。
type Kind string

const (
	// KindStep 表示 current/total 型阶段进度。
	KindStep Kind = "step"
	// KindPercent 表示百分比型实时进度。
	KindPercent Kind = "percent"
)

// Event 表示一条可被格式化或解析的统一进度事件。
type Event struct {
	Kind    Kind
	Stage   string
	Current int
	Total   int
	Percent float64
	Detail  string
}
