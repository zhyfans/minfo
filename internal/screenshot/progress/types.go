// Package progress 提供截图子模块共用的阶段进度与心跳辅助函数。

package progress

import screenshotruntime "minfo/internal/screenshot/runtime"

// LineHandler 处理进度子模块产生的单行实时日志。
type LineHandler = screenshotruntime.LineHandler
