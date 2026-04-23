// Package handlers 提供截图生成、上传和下载接口。

package handlers

import (
	"net/http"

	"minfo/internal/httpapi/transport"
)

// ScreenshotsHandler 会根据请求方法分发截图生成、预下载和文件下载逻辑。
func ScreenshotsHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		handleScreenshotZipDownload(w, r)
	case http.MethodHead:
		handleScreenshotZipDownload(w, r)
	case http.MethodPost:
		handleScreenshotsPost(w, r)
	default:
		transport.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}
