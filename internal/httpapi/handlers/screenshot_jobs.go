// Package handlers 提供截图后台任务的创建、取消与状态查询接口。

package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"minfo/internal/httpapi/transport"
)

// ScreenshotJobsHandler 负责创建新的截图后台任务，并立即返回任务 ID。
func ScreenshotJobsHandler(w http.ResponseWriter, r *http.Request) {
	if !transport.EnsurePost(w, r) {
		return
	}
	if err := transport.ParseForm(w, r); err != nil {
		writeScreenshotJobError(w, http.StatusBadRequest, err.Error())
		return
	}
	defer transport.CleanupMultipart(r)

	request, err := parseScreenshotFormRequest(r)
	if err != nil {
		writeScreenshotJobError(w, http.StatusBadRequest, err.Error())
		return
	}

	job, err := createScreenshotJob(request)
	if err != nil {
		if request.Cleanup != nil {
			request.Cleanup()
		}
		writeScreenshotJobError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeScreenshotJobResponse(w, http.StatusAccepted, job.snapshot())
}

// ScreenshotJobHandler 返回截图后台任务当前状态，或处理对已有任务的取消请求。
func ScreenshotJobHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		handleScreenshotJobGet(w, r)
	case http.MethodDelete:
		handleScreenshotJobDelete(w, r)
	default:
		writeScreenshotJobError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleScreenshotJobGet 返回截图后台任务当前状态，也会在完成后带上最终结果和日志。
func handleScreenshotJobGet(w http.ResponseWriter, r *http.Request) {
	jobID := parseScreenshotJobID(r)
	if jobID == "" || strings.Contains(jobID, "/") {
		writeScreenshotJobError(w, http.StatusNotFound, "job not found")
		return
	}

	job, ok := getScreenshotJob(jobID)
	if !ok {
		writeScreenshotJobError(w, http.StatusNotFound, "job not found")
		return
	}

	writeScreenshotJobResponse(w, http.StatusOK, job.snapshot())
}

// handleScreenshotJobDelete 会请求取消指定的截图后台任务，并返回取消后的最新快照。
func handleScreenshotJobDelete(w http.ResponseWriter, r *http.Request) {
	jobID := parseScreenshotJobID(r)
	if jobID == "" || strings.Contains(jobID, "/") {
		writeScreenshotJobError(w, http.StatusNotFound, "job not found")
		return
	}

	job, ok := getScreenshotJob(jobID)
	if !ok {
		writeScreenshotJobError(w, http.StatusNotFound, "job not found")
		return
	}

	job.requestCancel()
	writeScreenshotJobResponse(w, http.StatusOK, job.snapshot())
}

// parseScreenshotJobID 会从请求路径中提取截图后台任务 ID。
func parseScreenshotJobID(r *http.Request) string {
	jobID := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/api/screenshot-jobs/"))
	return strings.Trim(jobID, "/")
}

// writeScreenshotJobResponse 会把截图后台任务响应编码为 JSON，并显式关闭缓存。
func writeScreenshotJobResponse(w http.ResponseWriter, status int, payload transport.ScreenshotJobResponse) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

// writeScreenshotJobError 会把后台任务相关错误包装成统一 JSON 响应。
func writeScreenshotJobError(w http.ResponseWriter, status int, message string) {
	writeScreenshotJobResponse(w, status, transport.ScreenshotJobResponse{
		OK:    false,
		Error: message,
	})
}
